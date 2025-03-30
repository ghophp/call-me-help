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
func (s *SpeechToTextService) StreamingRecognize(ctx context.Context) (<-chan string, speechpb.Speech_StreamingRecognizeClient, error) {
	log.Printf("Starting streaming recognition with network diagnostics (ngrok-aware)")

	// Create output channel with generous buffer
	transcriptionChan := make(chan string)

	log.Printf("Attempting to establish STT stream connection...")
	stream, err := s.client.StreamingRecognize(ctx)
	if err != nil {
		log.Printf("Failed to create streaming recognition: %v", err)
		return nil, nil, err
	}

	stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_StreamingConfig{
			StreamingConfig: &speechpb.StreamingRecognitionConfig{
				Config: &speechpb.RecognitionConfig{
					Encoding:        speechpb.RecognitionConfig_MULAW,
					SampleRateHertz: 8000,
					LanguageCode:    "en-US",
				},
				InterimResults: true,
			},
		},
	})

	go s.ListenForResults(stream)

	return transcriptionChan, stream, nil
}

// ListenForResults listens for transcription results
func (s *SpeechToTextService) ListenForResults(stream speechpb.Speech_StreamingRecognizeClient) {
	go func() {
		log.Println("Starting to listen for Speech-to-Text results")
		for {
			resp, err := stream.Recv()
			if err == io.EOF {
				log.Println("Stream closed")
				return
			}
			if err != nil {
				log.Printf("Error receiving from stream: %v", err)
				return
			}

			// TODO: connect to transcription channel

			log.Printf("Received response with %d results", len(resp.Results))
			for _, result := range resp.Results {
				for _, alt := range result.Alternatives {
					isFinal := result.IsFinal
					status := "Interim"
					if isFinal {
						status = "Final"
					}
					log.Printf("Transcription (%s): %s", status, alt.Transcript)
				}
			}
		}
	}()
}
