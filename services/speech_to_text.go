package services

import (
	"context"
	"io"
	"log"

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
	client, err := speech.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	return &SpeechToTextService{
		client: client,
		config: config.Load(),
	}, nil
}

// Close closes the speech client
func (s *SpeechToTextService) Close() error {
	return s.client.Close()
}

// StreamingRecognize performs streaming speech recognition
func (s *SpeechToTextService) StreamingRecognize(ctx context.Context, audioStream io.Reader) (<-chan string, error) {
	stream, err := s.client.StreamingRecognize(ctx)
	if err != nil {
		return nil, err
	}

	// Send the initial configuration request
	if err := stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: &speechpb.StreamingRecognitionConfig{
				Config: &speechpb.RecognitionConfig{
					Encoding:        speechpb.RecognitionConfig_MULAW,
					SampleRateHertz: 8000, // Typical for telephony
					LanguageCode:    "en-US",
					Model:           "phone_call",
					UseEnhanced:     true,
				},
				InterimResults: false,
			},
		},
	}); err != nil {
		return nil, err
	}

	// Create a channel to send transcriptions back
	transcriptionChan := make(chan string)

	// Process audio stream and recognition results
	go func() {
		defer close(transcriptionChan)

		// Read audio in chunks and send to Google
		buf := make([]byte, 8192)
		for {
			n, err := audioStream.Read(buf)
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Printf("Error reading audio: %v", err)
				break
			}

			if err := stream.Send(&speechpb.StreamingRecognizeRequest{
				StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
					AudioContent: buf[:n],
				},
			}); err != nil {
				log.Printf("Error sending audio: %v", err)
				break
			}
		}

		if err := stream.CloseSend(); err != nil {
			log.Printf("Error closing stream: %v", err)
			return
		}

		// Receive streaming responses
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Printf("Error receiving: %v", err)
				break
			}

			for _, result := range resp.Results {
				if result.IsFinal {
					for _, alt := range result.Alternatives {
						transcriptionChan <- alt.Transcript
					}
				}
			}
		}
	}()

	return transcriptionChan, nil
}
