package services

import (
	"github.com/ghophp/call-me-help/config"
	"github.com/twilio/twilio-go"
	twilioApi "github.com/twilio/twilio-go/rest/api/v2010"
)

// TwilioService handles interactions with Twilio API
type TwilioService struct {
	client *twilio.RestClient
	config *config.Config
}

// NewTwilioService creates a new Twilio service
func NewTwilioService() *TwilioService {
	cfg := config.Load()

	// Create a new Twilio client
	client := twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: cfg.TwilioAccountSID,
		Password: cfg.TwilioAuthToken,
	})

	return &TwilioService{
		client: client,
		config: cfg,
	}
}

// GenerateTwiML generates TwiML for an incoming call
func (t *TwilioService) GenerateTwiML(callbackURL string) string {
	// Create a TwiML response that connects the call to our media stream
	return `<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Start>
    <Stream url="` + callbackURL + `"/>
  </Start>
  <Say>Hello, I'm your therapy assistant. How are you feeling today?</Say>
  <Pause length="60"/>
</Response>`
}

// SendMessage sends an SMS message using Twilio
func (t *TwilioService) SendMessage(to, message string) error {
	params := &twilioApi.CreateMessageParams{}
	params.SetTo(to)
	params.SetFrom(t.config.TwilioPhoneNumber)
	params.SetBody(message)

	_, err := t.client.Api.CreateMessage(params)
	return err
}
