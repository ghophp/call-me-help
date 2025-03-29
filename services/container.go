package services

// ServiceContainer holds all services used by the application
type ServiceContainer struct {
	SpeechToText   *SpeechToTextService
	TextToSpeech   *TextToSpeechService
	Gemini         *GeminiService
	Twilio         *TwilioService
	Conversation   *ConversationService
	ChannelManager *ChannelManager
}
