package services

import (
	"log"

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

	log.Printf("Initializing Twilio service with account SID: %s", maskString(cfg.TwilioAccountSID))

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
	log.Printf("Generating TwiML with Stream URL: %s", callbackURL)

	// Create a TwiML response that connects the call to our media stream
	// Add longer pauses and clearer instructions to encourage speaking
	twiml := `<?xml version="1.0" encoding="UTF-8"?>
<Response>
  <Start>
    <Stream url="` + callbackURL + `">
      <Parameter name="CallSid" value="{{CallSid}}" />
    </Stream>
  </Start>
  <Say voice="Polly.Joanna">Hello, I'm your therapy assistant. How are you feeling today?</Say>
</Response>`

	log.Printf("Generated TwiML response: %s", twiml)
	return twiml
}

// SendMessage sends an SMS message using Twilio
func (t *TwilioService) SendMessage(to, message string) error {
	log.Printf("Sending SMS to %s: %s", maskPhoneNumber(to), message)

	params := &twilioApi.CreateMessageParams{}
	params.SetTo(to)
	params.SetFrom(t.config.TwilioPhoneNumber)
	params.SetBody(message)

	resp, err := t.client.Api.CreateMessage(params)
	if err != nil {
		log.Printf("Error sending SMS: %v", err)
		return err
	}

	log.Printf("SMS sent successfully with SID: %s", *resp.Sid)
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
