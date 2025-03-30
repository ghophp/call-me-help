package services

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	texttospeech "cloud.google.com/go/texttospeech/apiv1"
	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
	"github.com/ghophp/call-me-help/config"
	"github.com/ghophp/call-me-help/logger"
)

// TextToSpeechService handles conversion of text to speech
type TextToSpeechService struct {
	client *texttospeech.Client
	config *config.Config
	log    *logger.Logger
}

// NewTextToSpeechService creates a new text-to-speech service
func NewTextToSpeechService(ctx context.Context) (*TextToSpeechService, error) {
	log := logger.Component("TextToSpeech")
	log.Info("Creating new Text-to-Speech service")

	client, err := texttospeech.NewClient(ctx)
	if err != nil {
		log.Error("Error creating Text-to-Speech client: %v", err)
		return nil, err
	}
	log.Info("Text-to-Speech client created successfully")

	return &TextToSpeechService{
		client: client,
		config: config.Load(),
		log:    log,
	}, nil
}

// Close closes the TTS client
func (t *TextToSpeechService) Close() error {
	t.log.Info("Closing Text-to-Speech client")
	return t.client.Close()
}

// SynthesizeSpeech converts text to audio
func (t *TextToSpeechService) SynthesizeSpeech(ctx context.Context, text string) ([]byte, error) {
	startTime := time.Now()
	t.log.Info("Synthesizing speech for text (%d chars): %q", len(text), text)

	req := texttospeechpb.SynthesizeSpeechRequest{
		Input: &texttospeechpb.SynthesisInput{
			InputSource: &texttospeechpb.SynthesisInput_Text{
				Text: text,
			},
		},
		Voice: &texttospeechpb.VoiceSelectionParams{
			LanguageCode: "en-US",
			SsmlGender:   texttospeechpb.SsmlVoiceGender_NEUTRAL,
			Name:         "en-US-Standard-I", // Using a specific voice for consistency
		},
		AudioConfig: &texttospeechpb.AudioConfig{
			AudioEncoding:   texttospeechpb.AudioEncoding_MULAW,
			SampleRateHertz: 8000, // 8kHz for telephony (Twilio requirement)
			EffectsProfileId: []string{
				"telephony-class-application", // Optimize for telephony
			},
		},
	}

	t.log.Debug("Configured TTS request: language=%s, gender=%s, encoding=%s, sampleRate=%d, voice=%s",
		req.Voice.LanguageCode,
		req.Voice.SsmlGender,
		req.AudioConfig.AudioEncoding,
		req.AudioConfig.SampleRateHertz,
		req.Voice.Name)

	// Create a timeout for the API call
	ttsCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	t.log.Debug("Calling Text-to-Speech API...")
	resp, err := t.client.SynthesizeSpeech(ttsCtx, &req)
	callDuration := time.Since(startTime)

	if err != nil {
		t.log.Error("Text-to-Speech API error after %v: %v", callDuration, err)
		return nil, err
	}

	t.log.Debug("Text-to-Speech API call completed in %v", callDuration)

	if resp == nil || resp.AudioContent == nil || len(resp.AudioContent) == 0 {
		t.log.Warn("Text-to-Speech returned empty audio content")
		return []byte{}, nil
	}

	t.log.Info("Successfully synthesized %d bytes of audio", len(resp.AudioContent))
	return resp.AudioContent, nil
}

// SaveAudioToFile saves audio content to a file
func (t *TextToSpeechService) SaveAudioToFile(callSID string, text string, audioData []byte) error {
	// Use the configured output directory
	outputDir := t.config.AudioOutputDirectory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.log.Error("Failed to create output directory: %v", err)
		return err
	}

	// Create a unique filename based on call SID and timestamp
	timestamp := time.Now().Format("20060102-150405.000")
	sanitizedText := sanitizeFilename(text)
	if len(sanitizedText) > 30 {
		sanitizedText = sanitizedText[:30] // Limit text length in filename
	}

	filename := fmt.Sprintf("%s/%s_%s_%s.raw", outputDir, callSID, timestamp, sanitizedText)

	// Save the audio data to file
	t.log.Info("Saving %d bytes of audio to file: %s", len(audioData), filename)
	if err := os.WriteFile(filename, audioData, 0644); err != nil {
		t.log.Error("Failed to save audio to file: %v", err)
		return err
	}

	t.log.Info("Successfully saved audio to file: %s", filename)
	return nil
}

// sanitizeFilename removes special characters from a string to make it safe for use in a filename
func sanitizeFilename(input string) string {
	// Replace spaces with underscores
	result := strings.ReplaceAll(input, " ", "_")

	// Remove non-alphanumeric characters
	reg := regexp.MustCompile("[^a-zA-Z0-9_]")
	result = reg.ReplaceAllString(result, "")

	return result
}
