package services

import (
	"github.com/ghophp/call-me-help/config"
	"github.com/ghophp/call-me-help/logger"
	"github.com/twilio/twilio-go"
	twilioApi "github.com/twilio/twilio-go/rest/api/v2010"
)

// TwilioService handles interactions with Twilio API
type TwilioService struct {
	client *twilio.RestClient
	config *config.Config
	log    *logger.Logger
}

// NewTwilioService creates a new Twilio service
func NewTwilioService() *TwilioService {
	cfg := config.Load()
	log := logger.Component("TwilioService")

	log.Info("Initializing Twilio service with account SID: %s", maskString(cfg.TwilioAccountSID))

	// Create a new Twilio client
	client := twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: cfg.TwilioAccountSID,
		Password: cfg.TwilioAuthToken,
	})

	return &TwilioService{
		client: client,
		config: cfg,
		log:    log,
	}
}

// GenerateTwiML generates TwiML for an incoming call
func (t *TwilioService) GenerateTwiML(callbackURL string) string {
	t.log.Info("Generating TwiML with Stream URL: %s", callbackURL)

	// Use <Connect> as specified in Twilio's documentation for bidirectional streaming
	twiml := `<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Connect>
    <Stream url="` + callbackURL + `" mediaFormat="audio/x-mulaw;rate=8000" />
  </Connect>
  <Say>Welcome to the therapy hotline.</Say>
  <Pause length="600"/>
</Response>`

	t.log.Info("Generated TwiML response with bidirectional streaming")
	return twiml
}

// SendMessage sends an SMS message using Twilio
func (t *TwilioService) SendMessage(to, message string) error {
	t.log.Info("Sending SMS to %s: %s", maskPhoneNumber(to), message)

	params := &twilioApi.CreateMessageParams{}
	params.SetTo(to)
	params.SetFrom(t.config.TwilioPhoneNumber)
	params.SetBody(message)

	resp, err := t.client.Api.CreateMessage(params)
	if err != nil {
		t.log.Error("Error sending SMS: %v", err)
		return err
	}

	t.log.Info("SMS sent successfully with SID: %s", *resp.Sid)
	return nil
}

// Helper function to mask sensitive data
func maskString(input string) string {
	if len(input) <= 8 {
		return "***masked***"
	}
	return input[:4] + "..." + input[len(input)-4:]
}

// Helper function to mask phone numbers
func maskPhoneNumber(phone string) string {
	if len(phone) <= 4 {
		return "****"
	}
	return "***" + phone[len(phone)-4:]
}
