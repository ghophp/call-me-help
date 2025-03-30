package services

import (
	"context"
	"fmt"
	"io"
	"log"
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
	log.Printf("Starting streaming recognition")
	stream, err := s.client.StreamingRecognize(ctx)
	if err != nil {
		log.Printf("Error creating streaming recognition: %v", err)
		return nil, err
	}
	log.Printf("Streaming recognition connection established")

	// Send the initial configuration request
	log.Printf("Sending initial configuration for streaming recognition")
	config := &speechpb.RecognitionConfig{
		Encoding:        speechpb.RecognitionConfig_MULAW,
		SampleRateHertz: 8000, // Typical for telephony
		LanguageCode:    "en-US",
		// Simplified model selection to ensure basic functionality
		Model:       "phone_call",
		UseEnhanced: true,
		// Add alternative language models
		AlternativeLanguageCodes: []string{"en-GB"},
		// Adjust speech contexts to be more sensitive to common phone call phrases
		SpeechContexts: []*speechpb.SpeechContext{
			{
				Phrases: []string{"hello", "hi", "help", "thank you", "thanks", "yes", "no", "okay", "not good", "feeling"},
				Boost:   10.0, // Increased boost to make detection more likely
			},
		},
		// Enable profanity filtering
		ProfanityFilter: false,
		// Enable word time offsets
		EnableWordTimeOffsets: true,
		// Enable automatic punctuation
		EnableAutomaticPunctuation: true,
		// Make it more sensitive to partial utterances
		MaxAlternatives: 3,
	}

	log.Printf("Using recognition config: encoding=%v, sampleRate=%v, language=%v, model=%v",
		config.Encoding, config.SampleRateHertz, config.LanguageCode, config.Model)

	if err := stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: &speechpb.StreamingRecognitionConfig{
				Config:         config,
				InterimResults: true, // Enable interim results to get faster responses
				// Lower stability threshold to get more interim results
				SingleUtterance: false, // Don't stop after a single utterance
			},
		},
	}); err != nil {
		log.Printf("Error sending configuration: %v", err)
		return nil, err
	}
	log.Printf("Initial configuration sent successfully")

	// Create a channel to send transcriptions back
	transcriptionChan := make(chan string, 10) // Add buffer to avoid blocking

	// Process audio stream and recognition results
	go func() {
		log.Printf("Starting audio processing goroutine")
		defer close(transcriptionChan)
		defer log.Printf("Audio processing goroutine completed")

		// Add a ticker to log progress
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		bytesProcessed := 0
		lastTranscriptionTime := time.Now()
		transcriptionCount := 0 // Track number of transcriptions generated
		silenceCount := 0       // Track consecutive silence chunks

		// Read audio in chunks and send to Google
		buf := make([]byte, 1024)
		for {
			select {
			case <-ctx.Done():
				log.Printf("Context canceled, stopping audio processing")
				return
			case <-ticker.C:
				elapsed := time.Since(lastTranscriptionTime)
				log.Printf("STT Status: Processed %d bytes total. No transcription for %v. Generated %d transcriptions so far.",
					bytesProcessed, elapsed, transcriptionCount)

				// If we've been processing for a while with no results, log a more detailed message
				if elapsed > 15*time.Second && bytesProcessed > 20000 {
					log.Printf("WARNING: Processed significant audio (%d bytes) but no transcription for %v. This may indicate an issue with STT service or audio quality.",
						bytesProcessed, elapsed)
				}
				// After 30 seconds with no results, suggest a potential issue
				if elapsed > 30*time.Second && bytesProcessed > 40000 && transcriptionCount == 0 {
					log.Printf("CRITICAL: No transcriptions after processing %d bytes for %v. Possible issues: 1) STT permissions 2) Audio format 3) No detectable speech 4) Network connectivity",
						bytesProcessed, elapsed)

					// Try sending a special metadata message to the stream
					log.Printf("Attempting to send a metadata message to the stream...")
					err := stream.Send(&speechpb.StreamingRecognizeRequest{
						StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
							AudioContent: generateTestAudioPattern(), // Send a test audio pattern that should be recognized
						},
					})
					if err != nil {
						log.Printf("Error sending test audio pattern: %v", err)
					}
				}
			default:
				// Continue with normal processing
			}

			n, err := audioStream.Read(buf)
			if err == io.EOF {
				log.Printf("End of audio stream reached")
				break
			}
			if err != nil {
				log.Printf("Error reading audio: %v", err)
				break
			}

			bytesProcessed += n
			if bytesProcessed%8000 == 0 { // Log every second of audio (8000 bytes at 8kHz for 8-bit samples)
				log.Printf("Processed %d bytes of audio (%.1f seconds)", bytesProcessed, float64(bytesProcessed)/8000.0)
			}

			// Check if audio seems to be all silence or constant values
			if n >= 10 {
				silenceCheck := true
				firstByte := buf[0]
				for i := 1; i < 10; i++ {
					if buf[i] != firstByte {
						silenceCheck = false
						break
					}
				}
				if silenceCheck {
					silenceCount++
					if silenceCount%50 == 0 { // Log only occasionally to reduce spam
						log.Printf("WARNING: Audio sample appears to be all constant value: %x. This might indicate silence or audio encoding issues", firstByte)

						// Only dump the first few bytes in a human-readable format to aid debugging
						if silenceCount <= 5 {
							hexDump := ""
							for i := 0; i < min(n, 16); i++ {
								hexDump += fmt.Sprintf("%02x ", buf[i])
							}
							log.Printf("Audio data bytes: %s", hexDump)
						}
					}
				} else {
					silenceCount = 0 // Reset counter when we see non-silence

					// Only log detailed audio data occasionally and only during early debugging
					if bytesProcessed < 10000 || bytesProcessed%50000 == 0 {
						log.Printf("Non-silence audio detected: first few bytes = %02x %02x %02x %02x...",
							buf[0], buf[1], buf[2], buf[3])
					}
				}
			}

			if err := stream.Send(&speechpb.StreamingRecognizeRequest{
				StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
					AudioContent: buf[:n],
				},
			}); err != nil {
				log.Printf("Error sending audio: %v", err)
				break
			}

			// Only log occasionally to reduce log spam
			if bytesProcessed%8000 < 160 { // Log roughly every second of audio
				log.Printf("Sent %d bytes to Speech-to-Text (total: %d)", n, bytesProcessed)
			}
		}

		log.Printf("Audio stream processing completed, total bytes: %d", bytesProcessed)
		log.Printf("Closing send stream")
		if err := stream.CloseSend(); err != nil {
			log.Printf("Error closing stream: %v", err)
			return
		}
		log.Printf("Send stream closed successfully")

		// Receive streaming responses
		log.Printf("Starting to receive transcription results")
		responseCount := 0

		for {
			select {
			case <-ctx.Done():
				log.Printf("Context canceled, stopping receiving")
				return
			default:
				// Continue with normal processing
			}

			log.Printf("Waiting for next transcription result")
			resp, err := stream.Recv()
			if err == io.EOF {
				log.Printf("End of recognition stream")
				break
			}
			if err != nil {
				log.Printf("ERROR receiving from STT: %v", err)
				break
			}

			responseCount++
			log.Printf("Received response #%d with %d results", responseCount, len(resp.Results))

			// Log empty response for debugging
			if len(resp.Results) == 0 {
				log.Printf("WARNING: Received empty result set from Speech-to-Text")
				continue
			}

			for i, result := range resp.Results {
				log.Printf("Result #%d: IsFinal=%v, Stability=%v, Alternatives=%d",
					i, result.IsFinal, result.Stability, len(result.Alternatives))

				if len(result.Alternatives) == 0 {
					log.Printf("WARNING: Result #%d has no alternatives", i)
					continue
				}

				// Send all results with reasonable confidence or stability
				if result.IsFinal || result.Stability > 0.3 { // Lowered stability threshold
					for j, alt := range result.Alternatives {
						confidenceStr := ""
						if alt.Confidence > 0 {
							confidenceStr = fmt.Sprintf(" (confidence: %.2f)", alt.Confidence)
						} else {
							confidenceStr = fmt.Sprintf(" (stability: %.2f)", result.Stability)
						}

						log.Printf("TRANSCRIPT #%d: %q%s", j, alt.Transcript, confidenceStr)

						if alt.Transcript != "" {
							log.Printf("SENDING TRANSCRIPTION TO CHANNEL: %q", alt.Transcript)
							transcriptionChan <- alt.Transcript
							lastTranscriptionTime = time.Now()
							transcriptionCount++
						} else {
							log.Printf("WARNING: Empty transcript in alternative #%d", j)
						}
					}
				} else {
					// Log low-stability results without sending them
					log.Printf("Low stability result (%.2f): %q",
						result.Stability,
						result.Alternatives[0].Transcript)
				}
			}
		}

		log.Printf("Recognition completed, received %d responses with %d total transcriptions",
			responseCount, transcriptionCount)
	}()

	return transcriptionChan, nil
}

// Helper function to generate a test audio pattern that should be recognized
func generateTestAudioPattern() []byte {
	// This is a simple audio pattern that varies enough to not be considered silence
	// but is not actually speech - just for testing the connection
	pattern := make([]byte, 320) // 40ms of audio at 8kHz

	// Create a pattern that varies (not just silence which is 0x7E in μ-law)
	for i := 0; i < len(pattern); i++ {
		// Create an alternating pattern
		if i%32 < 16 {
			pattern[i] = 0x40
		} else {
			pattern[i] = 0x80
		}
	}

	return pattern
}

// Helper function for min since Go <1.21 doesn't have min for ints in std lib
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
