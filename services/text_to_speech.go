package services

import (
	"context"

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
	client, err := texttospeech.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	return &TextToSpeechService{
		client: client,
		config: config.Load(),
	}, nil
}

// Close closes the TTS client
func (t *TextToSpeechService) Close() error {
	return t.client.Close()
}

// SynthesizeSpeech converts text to audio
func (t *TextToSpeechService) SynthesizeSpeech(ctx context.Context, text string) ([]byte, error) {
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

	resp, err := t.client.SynthesizeSpeech(ctx, &req)
	if err != nil {
		return nil, err
	}

	return resp.AudioContent, nil
}
