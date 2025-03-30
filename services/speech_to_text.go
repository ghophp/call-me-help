package services

import (
	"context"
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
		// Change to MULAW since Twilio sends MULAW format
		Encoding:        speechpb.RecognitionConfig_MULAW,
		SampleRateHertz: 8000, // Typical for telephony
		LanguageCode:    "en-US",
		// Use default model for telephony audio
		Model:       "default",
		UseEnhanced: true,
		// Simplify configuration to reduce potential issues
		MaxAlternatives:            1,
		EnableAutomaticPunctuation: true,
		// Add phrases that might appear in therapy conversations
		SpeechContexts: []*speechpb.SpeechContext{
			{
				Phrases: []string{
					"hello", "hi", "help", "thank you", "thanks", "yes", "no",
					"okay", "not good", "feeling", "better", "worse", "sad", "happy",
					"anxiety", "depression", "stressed", "therapy", "counseling",
				},
				Boost: 5.0,
			},
		},
	}

	log.Printf("Using recognition config: encoding=%v, sampleRate=%v, language=%v, model=%v",
		config.Encoding, config.SampleRateHertz, config.LanguageCode, config.Model)

	if err := stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: &speechpb.StreamingRecognitionConfig{
				Config:          config,
				InterimResults:  true,
				SingleUtterance: false,
			},
		},
	}); err != nil {
		log.Printf("Error sending configuration: %v", err)
		return nil, err
	}
	log.Printf("Initial configuration sent successfully")

	// Create a channel to send transcriptions back
	transcriptionChan := make(chan string, 20) // Increased buffer size

	// Process audio stream and recognition results
	go func() {
		log.Printf("Starting audio processing goroutine")
		defer close(transcriptionChan)
		defer log.Printf("Audio processing goroutine completed")

		// Read audio in chunks and send to Google
		buf := make([]byte, 8000) // Larger buffer for more efficient processing

		// Keep track of audio bytes sent for debugging
		var totalBytesSent int64

		for {
			select {
			case <-ctx.Done():
				log.Printf("Context canceled, stopping audio processing")
				if err := stream.CloseSend(); err != nil {
					log.Printf("Error closing stream: %v", err)
				}
				return
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

			if n == 0 {
				log.Printf("Warning: Read 0 bytes from audio stream")
				time.Sleep(10 * time.Millisecond) // Prevent tight loop
				continue
			}

			totalBytesSent += int64(n)
			log.Printf("Sending audio to STT: %d bytes (total: %d bytes)", n, totalBytesSent)

			// Send the audio chunk to Google Speech-to-Text
			if err := stream.Send(&speechpb.StreamingRecognizeRequest{
				StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
					AudioContent: buf[:n],
				},
			}); err != nil {
				log.Printf("Error sending audio: %v", err)
				break
			}

			// Small delay to pace the audio chunks
			time.Sleep(5 * time.Millisecond)
		}

		log.Printf("Audio stream sending complete, sent total of %d bytes", totalBytesSent)
		log.Printf("Closing send stream but keeping connection open for results")
		if err := stream.CloseSend(); err != nil {
			log.Printf("Error closing send stream: %v", err)
		}
	}()

	go func() {
		// Receive streaming responses
		log.Printf("Starting to receive transcription results")
		responseCount := 0
		emptyResultCount := 0

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

				// Send an error notification as a transcription
				transcriptionChan <- "[STT Error: " + err.Error() + "]"

				// Don't break on error - STT might recover
				continue
			}

			responseCount++

			// Handle empty responses
			if len(resp.Results) == 0 {
				emptyResultCount++
				log.Printf("Received empty result set #%d from Speech-to-Text", emptyResultCount)

				// Only send an empty result notification after several empty results
				if emptyResultCount == 10 {
					transcriptionChan <- "[No speech detected]"
					emptyResultCount = 0
				}
				continue
			}

			// Reset empty counter when we get results
			emptyResultCount = 0

			log.Printf("Received response #%d with %d results", responseCount, len(resp.Results))

			for i, result := range resp.Results {
				isFinal := result.IsFinal
				stability := result.Stability

				log.Printf("Result #%d: IsFinal=%v, Stability=%v, Alternatives=%d",
					i, isFinal, stability, len(result.Alternatives))

				if len(result.Alternatives) == 0 {
					log.Printf("Warning: Result #%d has no alternatives", i)
					continue
				}

				// Process each alternative
				for j, alt := range result.Alternatives {
					confidence := alt.Confidence
					transcript := alt.Transcript

					// Different log format for final vs interim results
					if isFinal {
						log.Printf("FINAL #%d.%d: %q (confidence: %.2f)", i, j, transcript, confidence)
					} else {
						log.Printf("INTERIM #%d.%d: %q (stability: %.2f)", i, j, transcript, stability)
					}

					// Only send reasonable transcripts
					if transcript != "" {
						// Send all final results
						if isFinal {
							log.Printf("Sending FINAL transcription: %q", transcript)
							transcriptionChan <- transcript
						} else if stability > 0.8 && j == 0 {
							// Only send the most stable interim results for the first alternative
							log.Printf("Sending stable INTERIM transcription: %q (stability: %.2f)", transcript, stability)
							transcriptionChan <- transcript
						}
					}
				}
			}
		}

		log.Printf("Recognition receiver completed, processed %d responses", responseCount)
	}()

	return transcriptionChan, nil
}
