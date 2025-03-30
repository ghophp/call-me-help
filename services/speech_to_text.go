package services

import (
	"context"
	"io"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/ghophp/call-me-help/config"
	"github.com/ghophp/call-me-help/logger"
)

// SpeechToTextService handles transcription of audio to text
type SpeechToTextService struct {
	client *speech.Client
	config *config.Config
	log    *logger.Logger
}

// NewSpeechToTextService creates a new speech-to-text service
func NewSpeechToTextService(ctx context.Context) (*SpeechToTextService, error) {
	log := logger.Component("SpeechToText")
	log.Info("Creating new Speech-to-Text service")

	client, err := speech.NewClient(ctx)
	if err != nil {
		log.Error("Error creating Speech-to-Text client: %v", err)
		return nil, err
	}
	log.Info("Speech-to-Text client created successfully")

	return &SpeechToTextService{
		client: client,
		config: config.Load(),
		log:    log,
	}, nil
}

// Close closes the speech client
func (s *SpeechToTextService) Close() error {
	s.log.Info("Closing Speech-to-Text client")
	return s.client.Close()
}

// StreamingRecognize performs streaming speech recognition
func (s *SpeechToTextService) StreamingRecognize(ctx context.Context) (<-chan string, speechpb.Speech_StreamingRecognizeClient, error) {
	s.log.Info("Starting streaming recognition")

	// Create output channel with generous buffer
	transcriptionChan := make(chan string, 1024)

	s.log.Debug("Attempting to establish STT stream connection...")
	stream, err := s.client.StreamingRecognize(ctx)
	if err != nil {
		s.log.Error("Failed to create streaming recognition: %v", err)
		return nil, nil, err
	}

	// Send configuration first
	err = stream.Send(&speechpb.StreamingRecognizeRequest{
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

	if err != nil {
		s.log.Error("Failed to send config to streaming recognition: %v", err)
		return nil, nil, err
	}

	// Start reading results in a goroutine
	go s.ListenForResults(stream, transcriptionChan)

	return transcriptionChan, stream, nil
}

// ListenForResults listens for transcription results
func (s *SpeechToTextService) ListenForResults(stream speechpb.Speech_StreamingRecognizeClient, transcriptionChan chan<- string) {
	s.log.Info("Starting to listen for Speech-to-Text results")

	defer func() {
		s.log.Info("Closing transcription channel")
		close(transcriptionChan)
	}()

	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			s.log.Info("Stream closed")
			return
		}
		if err != nil {
			s.log.Error("Error receiving from stream: %v", err)
			return
		}

		s.log.Debug("Received response with %d results", len(resp.Results))
		for _, result := range resp.Results {
			for _, alt := range result.Alternatives {
				isFinal := result.IsFinal
				status := "Interim"
				if isFinal {
					status = "Final"
				}

				transcript := alt.Transcript
				s.log.Info("Transcription (%s): %s", status, transcript)

				// Send transcript to the channel
				transcriptionChan <- transcript
			}
		}
	}
}
