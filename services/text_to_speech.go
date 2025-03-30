package services

import (
	"context"
	"log"
	"time"

	texttospeech "cloud.google.com/go/texttospeech/apiv1"
	"cloud.google.com/go/texttospeech/apiv1/texttospeechpb"
	"github.com/ghophp/call-me-help/config"
)

// TextToSpeechService handles conversion of text to speech
type TextToSpeechService struct {
	client *texttospeech.Client
	config *config.Config
}

// NewTextToSpeechService creates a new text-to-speech service
func NewTextToSpeechService(ctx context.Context) (*TextToSpeechService, error) {
	log.Printf("Creating new Text-to-Speech service")
	client, err := texttospeech.NewClient(ctx)
	if err != nil {
		log.Printf("Error creating Text-to-Speech client: %v", err)
		return nil, err
	}
	log.Printf("Text-to-Speech client created successfully")

	return &TextToSpeechService{
		client: client,
		config: config.Load(),
	}, nil
}

// Close closes the TTS client
func (t *TextToSpeechService) Close() error {
	log.Printf("Closing Text-to-Speech client")
	return t.client.Close()
}

// SynthesizeSpeech converts text to audio
func (t *TextToSpeechService) SynthesizeSpeech(ctx context.Context, text string) ([]byte, error) {
	startTime := time.Now()
	log.Printf("Synthesizing speech for text (%d chars): %q", len(text), text)

	req := texttospeechpb.SynthesizeSpeechRequest{
		Input: &texttospeechpb.SynthesisInput{
			InputSource: &texttospeechpb.SynthesisInput_Text{
				Text: text,
			},
		},
		Voice: &texttospeechpb.VoiceSelectionParams{
			LanguageCode: "en-US",
			SsmlGender:   texttospeechpb.SsmlVoiceGender_NEUTRAL,
		},
		AudioConfig: &texttospeechpb.AudioConfig{
			AudioEncoding:   texttospeechpb.AudioEncoding_MULAW,
			SampleRateHertz: 8000, // For telephony
		},
	}

	log.Printf("Configured TTS request: language=%s, gender=%s, encoding=%s, sampleRate=%d",
		req.Voice.LanguageCode,
		req.Voice.SsmlGender,
		req.AudioConfig.AudioEncoding,
		req.AudioConfig.SampleRateHertz)

	// Create a timeout for the API call
	ttsCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	log.Printf("Calling Text-to-Speech API...")
	resp, err := t.client.SynthesizeSpeech(ttsCtx, &req)
	callDuration := time.Since(startTime)

	if err != nil {
		log.Printf("Text-to-Speech API error after %v: %v", callDuration, err)
		return nil, err
	}

	log.Printf("Text-to-Speech API call completed in %v", callDuration)

	if resp == nil || resp.AudioContent == nil || len(resp.AudioContent) == 0 {
		log.Printf("WARNING: Text-to-Speech returned empty audio content")
		return []byte{}, nil
	}

	log.Printf("Successfully synthesized %d bytes of audio", len(resp.AudioContent))
	return resp.AudioContent, nil
}
