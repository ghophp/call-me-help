package services

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/ghophp/call-me-help/config"
)

// SpeechToTextService handles transcription of audio to text
type SpeechToTextService struct {
	client *speech.Client
	config *config.Config
}

// NewSpeechToTextService creates a new speech-to-text service
func NewSpeechToTextService(ctx context.Context) (*SpeechToTextService, error) {
	log.Printf("Creating new Speech-to-Text service")
	client, err := speech.NewClient(ctx)
	if err != nil {
		log.Printf("Error creating Speech-to-Text client: %v", err)
		return nil, err
	}
	log.Printf("Speech-to-Text client created successfully")

	return &SpeechToTextService{
		client: client,
		config: config.Load(),
	}, nil
}

// Close closes the speech client
func (s *SpeechToTextService) Close() error {
	log.Printf("Closing Speech-to-Text client")
	return s.client.Close()
}

// StreamingRecognize performs streaming speech recognition
func (s *SpeechToTextService) StreamingRecognize(ctx context.Context, audioStream io.Reader) (<-chan string, error) {
	log.Printf("Starting streaming recognition with network diagnostics (ngrok-aware)")

	// Create output channel with generous buffer
	transcriptionChan := make(chan string, 1024)

	// Create a new recognition stream with timeout context to detect slow connections
	dialCtx, cancelDial := context.WithTimeout(ctx, 10*time.Second)
	defer cancelDial()

	log.Printf("Attempting to establish STT stream connection...")
	startConnTime := time.Now()
	stream, err := s.client.StreamingRecognize(dialCtx)
	connLatency := time.Since(startConnTime)

	if err != nil {
		log.Printf("Failed to create streaming recognition: %v", err)
		if dialCtx.Err() != nil {
			log.Printf("Connection timed out - this could be due to ngrok latency")
			return nil, fmt.Errorf("connection timeout (possibly ngrok related): %v", err)
		}
		return nil, err
	}
	log.Printf("STT stream established successfully (latency: %v)", connLatency)

	// Check if latency is high (potential ngrok issue)
	if connLatency > 2*time.Second {
		log.Printf("WARNING: High connection latency detected (%v). This may impact STT performance if using ngrok.", connLatency)
		transcriptionChan <- "[Info: High network latency detected, transcription may be delayed]"
	}

	// Send configuration with timeout to detect potential network issues
	log.Printf("Sending STT configuration...")
	configCtx, cancelConfig := context.WithTimeout(ctx, 5*time.Second)
	defer cancelConfig()

	configStartTime := time.Now()
	configErr := make(chan error, 1)

	go func() {
		configErr <- stream.Send(&speechpb.StreamingRecognizeRequest{
			StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
				StreamingConfig: &speechpb.StreamingRecognitionConfig{
					Config: &speechpb.RecognitionConfig{
						Encoding:                   speechpb.RecognitionConfig_MULAW,
						SampleRateHertz:            8000,
						LanguageCode:               "en-US",
						EnableAutomaticPunctuation: true,
						Model:                      "default", // Using default model instead of phone_call
						UseEnhanced:                true,      // Enable enhanced models
						// Add speech adaptation to improve recognition
						Adaptation: &speechpb.SpeechAdaptation{
							PhraseSets: []*speechpb.PhraseSet{
								{
									Phrases: []*speechpb.PhraseSet_Phrase{
										{Value: "hello"},
										{Value: "help"},
										{Value: "thank you"},
										{Value: "yes"},
										{Value: "no"},
										{Value: "okay"},
										{Value: "not feeling good"},
										{Value: "feeling sad"},
										{Value: "feeling anxious"},
										{Value: "feeling depressed"},
									},
									Boost: 20.0,
								},
							},
						},
					},
					InterimResults:  true,
					SingleUtterance: false,
				},
			},
		})
	}()

	select {
	case err := <-configErr:
		if err != nil {
			log.Printf("Failed to send config: %v", err)
			return nil, err
		}
		configLatency := time.Since(configStartTime)
		log.Printf("STT configuration sent successfully (latency: %v)", configLatency)

		// Check if config latency is high
		if configLatency > 1*time.Second {
			log.Printf("WARNING: High configuration latency detected (%v). This may indicate network issues.", configLatency)
		}
	case <-configCtx.Done():
		return nil, fmt.Errorf("timeout sending STT configuration - ngrok might be causing delays")
	}

	// Use a WaitGroup to manage goroutines
	var wg sync.WaitGroup

	// Start a goroutine to send audio data
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if err := stream.CloseSend(); err != nil {
				log.Printf("Error closing send stream: %v", err)
			} else {
				log.Printf("Successfully closed send stream")
			}
		}()

		buffer := make([]byte, 1024)
		audioChunkCount := 0
		totalBytes := 0
		startTime := time.Now()
		lastLogTime := startTime

		// Track sending latency to detect network issues
		var sendLatencies []time.Duration
		var lastSendTime time.Time
		var currentSendStart time.Time
		var consecutiveSlowSends int

		// Monitor network health
		networkHealthy := true

		for {
			select {
			case <-ctx.Done():
				log.Printf("Context canceled, stopping audio sending")
				return
			default:
				// Read from audio stream
				n, err := audioStream.Read(buffer)

				if err == io.EOF {
					log.Printf("End of audio stream")
					return
				}

				if err != nil {
					log.Printf("Error reading audio: %v", err)
					return
				}

				if n > 0 {
					// Debug first chunk
					if audioChunkCount == 0 {
						log.Printf("First audio chunk: %v", formatBytes(buffer[:min(n, 16)]))
					}

					// Send audio chunk immediately
					currentSendStart = time.Now()
					if err := stream.Send(&speechpb.StreamingRecognizeRequest{
						StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
							AudioContent: buffer[:n],
						},
					}); err != nil {
						log.Printf("Error sending audio chunk: %v", err)

						// Check if this is a network timeout or connectivity issue (common with ngrok)
						if err.Error() == "rpc error: code = Unavailable" ||
							err.Error() == "rpc error: code = DeadlineExceeded" {
							log.Printf("Network connectivity issue detected - possibly related to ngrok")
							if networkHealthy {
								select {
								case <-ctx.Done():
								case transcriptionChan <- "[Error: Network connectivity issue detected, transcription may be affected]":
								}
								networkHealthy = false
							}
						}
						return
					}

					// Calculate send latency
					sendLatency := time.Since(currentSendStart)
					sendLatencies = append(sendLatencies, sendLatency)

					// If not the first send, calculate the time between sends
					if !lastSendTime.IsZero() {
						sendInterval := currentSendStart.Sub(lastSendTime)

						// If the interval is too long, we might have network issues
						if sendInterval > 500*time.Millisecond && audioChunkCount > 10 {
							log.Printf("WARNING: Large gap between audio sends: %v", sendInterval)
						}
					}
					lastSendTime = time.Now()

					// Check for consecutive slow sends (potential ngrok issue)
					if sendLatency > 100*time.Millisecond {
						consecutiveSlowSends++
						if consecutiveSlowSends >= 5 && networkHealthy {
							log.Printf("WARNING: Detected %d consecutive slow sends, network may be congested", consecutiveSlowSends)
							select {
							case <-ctx.Done():
							case transcriptionChan <- "[Warning: Network appears congested, transcription quality may be affected]":
							}
							networkHealthy = false
						}
					} else {
						consecutiveSlowSends = 0

						// Network has recovered
						if !networkHealthy && consecutiveSlowSends == 0 {
							networkHealthy = true
							log.Printf("Network performance has improved")
						}
					}

					audioChunkCount++
					totalBytes += n

					// Log stats periodically
					if time.Since(lastLogTime) > time.Second {
						bytesPerSecond := float64(totalBytes) / time.Since(startTime).Seconds()

						// Calculate average send latency
						var avgLatency time.Duration
						if len(sendLatencies) > 0 {
							sum := time.Duration(0)
							for _, lat := range sendLatencies {
								sum += lat
							}
							avgLatency = sum / time.Duration(len(sendLatencies))

							// Only keep recent latencies
							if len(sendLatencies) > 50 {
								sendLatencies = sendLatencies[len(sendLatencies)-50:]
							}
						}

						log.Printf("Sent %d chunks (%d bytes, %.1f bytes/sec), avg send latency: %v",
							audioChunkCount, totalBytes, bytesPerSecond, avgLatency)
						lastLogTime = time.Now()
					}
				}
			}
		}
	}()

	// Start a goroutine to receive transcriptions
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(transcriptionChan)

		var lastTranscript string
		responseCount := 0
		transcriptionCount := 0
		lastResponseTime := time.Now()

		// Track potential ngrok issues
		longResponseGaps := 0

		// Set a timer to warn if no responses are received
		responseTimer := time.NewTimer(10 * time.Second)
		receivedFirstResponse := false

		go func() {
			<-responseTimer.C
			if !receivedFirstResponse {
				log.Printf("WARNING: No responses received from STT after 10 seconds - this could be an ngrok issue")
				select {
				case <-ctx.Done():
				case transcriptionChan <- "[WARNING: No response from Speech-to-Text API - check network connection]":
				}
			}
		}()

		for {
			select {
			case <-ctx.Done():
				log.Printf("Context canceled, stopping transcription receiver")
				return
			default:
				// Receive response from STT with timeout to detect network issues
				recvCtx, cancelRecv := context.WithTimeout(ctx, 30*time.Second)
				recvStartTime := time.Now()

				// Calculate time since last response
				timeSinceLastResponse := time.Since(lastResponseTime)
				if timeSinceLastResponse > 5*time.Second && responseCount > 0 {
					longResponseGaps++
					if longResponseGaps == 1 {
						log.Printf("WARNING: Long gap since last STT response (%v) - possible network issue", timeSinceLastResponse)
					}

					// If we've had multiple long gaps, warn the user
					if longResponseGaps == 3 {
						select {
						case <-ctx.Done():
						case transcriptionChan <- "[Warning: Experiencing delays in speech recognition responses]":
						}
					}
				} else {
					longResponseGaps = 0
				}

				// Create a channel to receive the response
				respCh := make(chan *speechpb.StreamingRecognizeResponse, 1)
				errCh := make(chan error, 1)

				go func() {
					resp, err := stream.Recv()
					if err != nil {
						errCh <- err
					} else {
						respCh <- resp
					}
				}()

				// Wait for response or timeout
				var resp *speechpb.StreamingRecognizeResponse
				var err error

				select {
				case resp = <-respCh:
					// No error, continue processing
				case err = <-errCh:
					// Handle error below
				case <-recvCtx.Done():
					if responseCount > 0 {
						log.Printf("Timeout waiting for STT response - possible network issue")
						select {
						case <-ctx.Done():
						case transcriptionChan <- "[Error: STT response timeout - check network connection]":
						}
					}
					cancelRecv()
					continue
				}

				// Measure response latency
				responseLatency := time.Since(recvStartTime)
				lastResponseTime = time.Now()

				// Cancel the warning timer once we get first response
				if !receivedFirstResponse {
					if !responseTimer.Stop() {
						<-responseTimer.C
					}
					receivedFirstResponse = true
					log.Printf("Received first STT response (latency: %v)", responseLatency)
				}

				// Check for high response latency
				if responseLatency > 1*time.Second && responseCount > 0 {
					log.Printf("WARNING: High STT response latency: %v", responseLatency)
				}

				if err == io.EOF {
					log.Printf("End of STT stream")
					cancelRecv()
					return
				}

				if err != nil {
					log.Printf("Error receiving STT response: %v", err)
					cancelRecv()

					// Only send errors through channel if it's not due to context cancellation
					if ctx.Err() == nil {
						select {
						case <-ctx.Done():
						case transcriptionChan <- fmt.Sprintf("[STT Error: %v]", err):
						}
						// Brief pause to avoid rapid error loops
						time.Sleep(100 * time.Millisecond)
					} else {
						return
					}
					continue
				}

				responseCount++
				cancelRecv()

				// Log some details about the response
				if len(resp.Results) > 0 {
					for _, result := range resp.Results {
						if len(result.Alternatives) == 0 {
							continue
						}

						transcript := result.Alternatives[0].Transcript

						// Skip empty or duplicate transcripts
						if transcript == "" || transcript == lastTranscript {
							continue
						}

						// Mark whether this is final or interim
						stability := float32(0)
						if !result.IsFinal {
							stability = result.Stability
							log.Printf("INTERIM (%d): %q (stability: %.2f)",
								responseCount, transcript, stability)

							// Skip low-stability interim results
							if stability < 0.5 {
								continue
							}
						} else {
							log.Printf("FINAL (%d): %q (confidence: %.2f)",
								responseCount, transcript, result.Alternatives[0].Confidence)
						}

						// Send the transcript
						select {
						case <-ctx.Done():
							return
						case transcriptionChan <- transcript:
							transcriptionCount++
							lastTranscript = transcript
						}
					}
				} else {
					if responseCount%10 == 0 {
						log.Printf("Received empty response #%d", responseCount)
					}
				}
			}
		}
	}()

	// Start a cleanup goroutine
	go func() {
		<-ctx.Done()
		log.Printf("Parent context canceled, cleaning up")

		// Wait for goroutines to finish with timeout
		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			log.Printf("All STT goroutines completed")
		case <-time.After(5 * time.Second):
			log.Printf("Timeout waiting for STT goroutines to complete")
		}
	}()

	return transcriptionChan, nil
}

// Helper function to format binary data for logging
func formatBytes(data []byte) string {
	if len(data) == 0 {
		return "[]"
	}

	s := "["
	for i, b := range data {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("0x%02x", b)
	}
	s += "]"
	return s
}

// Helper function for min value
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
