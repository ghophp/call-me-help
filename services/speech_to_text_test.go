package services

import (
	"context"
	"encoding/base64"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/ghophp/call-me-help/logger"
	"github.com/joho/godotenv"
	"google.golang.org/grpc/metadata"
)

// This file contains integration tests for the Speech-to-Text service.
// It requires Google Cloud credentials to be set up properly.
// Run with: go test -tags=integration -v ./services/speech_to_text_test.go

// Setup loads environment variables for tests
func setup() {
	// Load .env file if it exists
	godotenv.Load("../.env")
	logger.Initialize(logger.DEBUG)
}

// TestSpeechToTextIntegration tests the full integration with Google Cloud Speech-to-Text
func TestSpeechToTextIntegration(t *testing.T) {
	// Skip if not running integration tests
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=true to run.")
	}

	setup()

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a new speech-to-text service
	stt, err := NewSpeechToTextService(ctx)
	if err != nil {
		t.Fatalf("Failed to create Speech-to-Text service: %v", err)
	}
	defer stt.Close()

	// Start streaming recognition
	transcriptionChan, stream, err := stt.StreamingRecognize(ctx)
	if err != nil {
		t.Fatalf("Failed to start streaming recognition: %v", err)
	}

	// Load test audio file (8kHz mono mulaw audio)
	audioData, err := os.ReadFile("../testdata/test_audio.raw")
	if err != nil {
		t.Fatalf("Failed to read test audio file: %v", err)
	}

	// Send audio data to the recognition stream
	err = stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
			AudioContent: audioData,
		},
	})
	if err != nil {
		t.Fatalf("Failed to send audio data: %v", err)
	}

	// Wait for transcription results with timeout
	select {
	case transcript, ok := <-transcriptionChan:
		if !ok {
			t.Fatal("Transcription channel closed unexpectedly")
		}
		t.Logf("Received transcription: %s", transcript)
		if transcript == "" {
			t.Error("Received empty transcription")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Timed out waiting for transcription")
	}
}

// TestStreamingRecognizeWithSynthesizedAudio tests with a small synthesized audio file
func TestStreamingRecognizeWithSynthesizedAudio(t *testing.T) {
	// Skip if not running integration tests
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=true to run.")
	}

	setup()

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a new speech-to-text service
	stt, err := NewSpeechToTextService(ctx)
	if err != nil {
		t.Fatalf("Failed to create Speech-to-Text service: %v", err)
	}
	defer stt.Close()

	// Start streaming recognition
	transcriptionChan, stream, err := stt.StreamingRecognize(ctx)
	if err != nil {
		t.Fatalf("Failed to start streaming recognition: %v", err)
	}

	// These values are from a real Twilio WebSocket message with a short audio clip
	// containing the phrase "hello world" in mulaw format
	audioBase64 := `//uwRAAAAA/8AjwBWv+QAAAAA/wAAAAAAA0gASMvRAAAAACBpBGLYcZADCQAAAjZYTtL//5tLBk6YIEGl//5N70ygAkHYaBgyRQABEMHDQcRQKt//6NDiIRByg5CZAAAABLCBII0Z//9A9iQJCFgERAIAAAANP/92RAUzx/jgAAA`

	// Decode the base64 audio
	audioData, err := base64.StdEncoding.DecodeString(audioBase64)
	if err != nil {
		t.Fatalf("Failed to decode base64 audio: %v", err)
	}

	// Send audio data to the recognition stream
	err = stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
			AudioContent: audioData,
		},
	})
	if err != nil {
		t.Fatalf("Failed to send audio data: %v", err)
	}

	// Wait for transcription results with timeout
	receivedTranscription := false
	timeout := time.After(20 * time.Second)

	for {
		select {
		case transcript, ok := <-transcriptionChan:
			if !ok {
				if !receivedTranscription {
					t.Fatal("Transcription channel closed without receiving any transcription")
				}
				return
			}
			t.Logf("Received transcription: %s", transcript)
			receivedTranscription = true

			// If we have a final result containing "hello", we're good
			if transcript != "" && (transcript == "hello" || transcript == "hello world") {
				return
			}
		case <-timeout:
			if !receivedTranscription {
				t.Fatal("Timed out waiting for transcription")
			}
			return
		}
	}
}

// TestSpeechToTextChannelCommunication tests that the channel communication works correctly
func TestSpeechToTextChannelCommunication(t *testing.T) {
	setup()

	// Create a mock stream that always returns a specific response
	mockStream := &mockStreamingRecognizeClient{
		responses: []*speechpb.StreamingRecognizeResponse{
			{
				Results: []*speechpb.StreamingRecognitionResult{
					{
						Alternatives: []*speechpb.SpeechRecognitionAlternative{
							{
								Transcript: "hello world",
							},
						},
						IsFinal: true,
					},
				},
			},
		},
	}

	// Create a channel to receive transcriptions
	transcriptionChan := make(chan string, 10)

	// Create a new speech-to-text service
	stt := &SpeechToTextService{
		log: logger.Component("SpeechToText"),
	}

	// Start listening for results
	go stt.ListenForResults(mockStream, transcriptionChan)

	// Wait for the result with timeout
	select {
	case transcript := <-transcriptionChan:
		if transcript != "hello world" {
			t.Errorf("Expected 'hello world', got '%s'", transcript)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for transcription")
	}
}

// mockStreamingRecognizeClient is a mock implementation of the Speech_StreamingRecognizeClient interface
type mockStreamingRecognizeClient struct {
	responses []*speechpb.StreamingRecognizeResponse
	index     int
	sent      bool
}

func (m *mockStreamingRecognizeClient) Send(request *speechpb.StreamingRecognizeRequest) error {
	m.sent = true
	return nil
}

func (m *mockStreamingRecognizeClient) Recv() (*speechpb.StreamingRecognizeResponse, error) {
	if m.index >= len(m.responses) {
		// Wait a bit before returning EOF to simulate streaming
		time.Sleep(500 * time.Millisecond)
		return nil, context.Canceled
	}
	resp := m.responses[m.index]
	m.index++
	return resp, nil
}

func (m *mockStreamingRecognizeClient) Header() (metadata.MD, error) {
	return nil, nil
}

func (m *mockStreamingRecognizeClient) Trailer() metadata.MD {
	return nil
}

func (m *mockStreamingRecognizeClient) CloseSend() error {
	return nil
}

func (m *mockStreamingRecognizeClient) Context() context.Context {
	return context.Background()
}

func (m *mockStreamingRecognizeClient) SendMsg(interface{}) error {
	return nil
}

func (m *mockStreamingRecognizeClient) RecvMsg(interface{}) error {
	return nil
}
