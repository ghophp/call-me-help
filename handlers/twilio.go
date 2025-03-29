package handlers

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/ghophp/call-me-help/services"
)

// TwilioStreamEvent represents a stream event from Twilio
type TwilioStreamEvent struct {
	Event       string `json:"event"`
	StreamSid   string `json:"streamSid"`
	CallSid     string `json:"callSid"`
	AccountSid  string `json:"accountSid"`
	MediaChunk  string `json:"media"`
	SequenceNum int    `json:"sequenceNumber"`
	Start       bool   `json:"start"`
	End         bool   `json:"end"`
	Payload     []byte `json:"-"`
}

// HandleIncomingCall handles an incoming call webhook from Twilio
func HandleIncomingCall(svc *services.ServiceContainer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Received call webhook from Twilio. URL: %s, Method: %s", r.URL.String(), r.Method)

		// Log all headers
		log.Printf("Request headers: %v", r.Header)

		if err := r.ParseForm(); err != nil {
			log.Printf("Error parsing form: %v", err)
			http.Error(w, "Could not parse form", http.StatusBadRequest)
			return
		}

		// Log all form fields
		log.Printf("Form data: %v", r.Form)

		// Get call information
		callSID := r.FormValue("CallSid")
		if callSID == "" {
			log.Printf("Missing CallSid in request")
			http.Error(w, "Missing CallSid", http.StatusBadRequest)
			return
		}

		log.Printf("Call received with SID: %s", callSID)

		// Create channels for this call
		log.Printf("Creating channels for call %s", callSID)
		svc.ChannelManager.CreateChannels(callSID)

		// Get the callback URL for the media stream
		// For Ngrok, we need to use the host as provided in the request
		// and use wss:// (WebSocket Secure) scheme
		host := r.Host

		// Check if it's an ngrok URL and use the proper scheme
		var wsScheme string
		if strings.Contains(host, "ngrok") {
			// For ngrok, we need to use wss directly
			wsScheme = "wss"
		} else {
			// For non-ngrok, infer from the request
			wsScheme = "ws"
			if r.TLS != nil {
				wsScheme = "wss"
			}
		}

		// Don't include callSid in URL - it will be passed in Stream parameters
		callbackURL := wsScheme + "://" + host + "/ws"
		log.Printf("WebSocket callback URL: %s", callbackURL)

		// Generate TwiML response with the stream URL
		twiml := svc.Twilio.GenerateTwiML(callbackURL)
		log.Printf("Generated TwiML: %s", twiml)

		w.Header().Set("Content-Type", "text/xml")
		w.Write([]byte(twiml))

		// Log the start of a new call
		log.Printf("New call started: %s", callSID)
	}
}

// HandleTwilioStream handles a stream event from Twilio
func HandleTwilioStream(svc *services.ServiceContainer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Received stream event from Twilio. URL: %s, Method: %s", r.URL.String(), r.Method)

		// Read and parse the JSON body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("Error reading request body: %v", err)
			http.Error(w, "Error reading request body", http.StatusInternalServerError)
			return
		}

		log.Printf("Stream event body: %s", string(body))

		var event TwilioStreamEvent
		if err := json.Unmarshal(body, &event); err != nil {
			log.Printf("Error parsing JSON: %v", err)
			http.Error(w, "Error parsing JSON", http.StatusBadRequest)
			return
		}

		// Handle different stream events
		switch event.Event {
		case "start":
			log.Printf("Stream started: %s for call %s", event.StreamSid, event.CallSid)
			w.WriteHeader(http.StatusOK)

		case "media":
			log.Printf("Media received for call %s", event.CallSid)
			// Get channels for this call
			channels, ok := svc.ChannelManager.GetChannels(event.CallSid)
			if !ok {
				log.Printf("No channels found for call %s", event.CallSid)
				http.Error(w, "No channels found for call", http.StatusNotFound)
				return
			}

			// Decode base64 data
			if err := json.Unmarshal([]byte(event.MediaChunk), &event.Payload); err != nil {
				log.Printf("Error decoding media: %v", err)
				http.Error(w, "Error decoding media", http.StatusBadRequest)
				return
			}

			// Add audio data to buffer
			channels.AppendAudioData(event.Payload)
			w.WriteHeader(http.StatusOK)

		case "stop":
			log.Printf("Stream stopped: %s for call %s", event.StreamSid, event.CallSid)

			// Clean up channels when the stream stops
			svc.ChannelManager.RemoveChannels(event.CallSid)
			w.WriteHeader(http.StatusOK)

		default:
			log.Printf("Unknown event: %s", event.Event)
			w.WriteHeader(http.StatusOK)
		}
	}
}
