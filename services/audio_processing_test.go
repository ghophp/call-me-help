package services

import (
	"context"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/ghophp/call-me-help/logger"
	"github.com/joho/godotenv"
)

// TestCompleteAudioProcessingFlow tests the complete flow from STT to Gemini to TTS
func TestCompleteAudioProcessingFlow(t *testing.T) {
	// Skip if not running integration tests
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=true to run.")
	}

	// Load .env file if it exists
	_ = godotenv.Load("../.env")
	logger.Initialize(logger.DEBUG)

	// Create a context with timeout for the test
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Initialize services
	t.Log("Initializing Speech-to-Text service...")
	stt, err := NewSpeechToTextService(ctx)
	if err != nil {
		t.Fatalf("Failed to create Speech-to-Text service: %v", err)
	}
	defer stt.Close()

	t.Log("Initializing Gemini service...")
	gemini, err := NewGeminiService(ctx)
	if err != nil {
		t.Fatalf("Failed to create Gemini service: %v", err)
	}
	defer gemini.Close()

	t.Log("Initializing Text-to-Speech service...")
	tts, err := NewTextToSpeechService(ctx)
	if err != nil {
		t.Fatalf("Failed to create Text-to-Speech service: %v", err)
	}
	defer tts.Close()

	t.Log("Initializing Conversation service...")
	conversationService := NewConversationService()

	t.Log("Creating test conversation...")
	conversation := conversationService.GetOrCreateConversation("test-call-id")

	// Test 1: Process a simple transcription through the entire flow
	transcription := "I'm feeling anxious today. Can you help me?"
	t.Logf("Test transcription: %q", transcription)

	// Add the transcription to the conversation history
	conversation.AddUserMessage(transcription)
	history := conversation.GetFormattedHistory()
	t.Logf("Conversation history has %d messages", len(history))

	// Generate response using Gemini
	t.Log("Generating response with Gemini...")
	startTime := time.Now()
	response, err := gemini.GenerateResponse(ctx, transcription, history)
	elapsed := time.Since(startTime)

	if err != nil {
		t.Fatalf("Failed to generate response: %v", err)
	}
	t.Logf("Generated response in %v: %q", elapsed, response)

	// Verify the response seems reasonable
	if len(response) < 10 {
		t.Errorf("Generated response is too short: %q", response)
	}

	// Add the response to the conversation
	conversation.AddTherapistMessage(response)

	// Convert the response to speech
	t.Log("Converting response to speech...")
	startTime = time.Now()
	audioData, err := tts.SynthesizeSpeech(ctx, response)
	elapsed = time.Since(startTime)

	if err != nil {
		t.Fatalf("Failed to synthesize speech: %v", err)
	}
	t.Logf("Synthesized %d bytes of audio in %v", len(audioData), elapsed)

	// Verify the audio data
	if len(audioData) == 0 {
		t.Error("Generated audio data is empty")
	}

	// Test 2: Test the STT streaming recognition with TTS output
	t.Log("Testing STT streaming recognition...")

	// Start speech recognition
	transcriptionChan, stream, err := stt.StreamingRecognize(ctx)
	if err != nil {
		t.Fatalf("Failed to start streaming recognition: %v", err)
	}

	// Start a goroutine to collect transcriptions
	go func() {
		for transcript := range transcriptionChan {
			t.Logf("Received transcription: %q", transcript)
		}
	}()

	// Send the previously generated audio back to STT
	// This is a loopback test: TTS -> STT
	t.Log("Sending synthesized audio back to STT...")
	stream.Send(&speechpb.StreamingRecognizeRequest{
		StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
			AudioContent: audioData,
		},
	})

	// Wait for some time to allow processing
	time.Sleep(5 * time.Second)

	t.Log("Complete audio processing flow test completed")
}
