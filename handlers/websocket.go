package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ghophp/call-me-help/services"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for Twilio connections
		log.Printf("WebSocket origin check: %s", r.Header.Get("Origin"))
		return true
	},
}

// TwilioWSEvent represents a WebSocket event from Twilio
type TwilioWSEvent struct {
	Event          string       `json:"event"`
	SequenceNumber string       `json:"sequenceNumber"`
	StreamSid      string       `json:"streamSid"`
	Media          *TwilioMedia `json:"media,omitempty"`
	Stop           *TwilioStop  `json:"stop,omitempty"`
}

// TwilioMedia represents media data in a Twilio WebSocket event
type TwilioMedia struct {
	Track     string `json:"track"`
	Chunk     string `json:"chunk"`
	Timestamp string `json:"timestamp"`
	Payload   string `json:"payload"` // Base64 encoded audio data
}

// TwilioStop represents the stop event data
type TwilioStop struct {
	AccountSid string `json:"accountSid"`
	CallSid    string `json:"callSid"`
}

// checkAudioFormat performs basic validation on audio data
func checkAudioFormat(data []byte) {
	// Log the first few bytes to help debug format issues
	if len(data) > 16 {
		log.Printf("Audio header bytes: [% x]", data[:16])
	} else if len(data) > 0 {
		log.Printf("Audio bytes (too short): [% x]", data)
	} else {
		log.Printf("Warning: Empty audio data")
	}

	// Check for silence/empty audio
	if len(data) > 0 {
		allSame := true
		firstByte := data[0]
		for _, b := range data {
			if b != firstByte {
				allSame = false
				break
			}
		}
		if allSame {
			log.Printf("Warning: Audio data appears to be silence or constant value: %02x", firstByte)
		}
	}
}

// HandleWebSocket handles WebSocket connections for streaming audio
func HandleWebSocket(svc *services.ServiceContainer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("WebSocket connection request received: %s", r.URL.String())

		// Extract Twilio Connection parameters from all possible sources
		var callSID string
		var parameters map[string]string

		// First parse any query parameters
		query := r.URL.Query()
		log.Printf("WebSocket URL query parameters: %v", query)

		// Check URL query parameters
		callSID = query.Get("callSid")
		if callSID == "" {
			callSID = query.Get("CallSid")
		}

		// Attempt to parse any parameters from headers
		// Twilio might send parameters as X-Twilio-Params
		twilioParamsHeader := r.Header.Get("X-Twilio-Params")
		if twilioParamsHeader != "" {
			log.Printf("Found Twilio params header: %s", twilioParamsHeader)
			parameters = parseParameters(twilioParamsHeader)

			// Check if CallSid is in the parameters
			if sid, ok := parameters["CallSid"]; ok && sid != "" {
				callSID = sid
				log.Printf("Found CallSid %s in parameters", callSID)
			}
		}

		// Look for custom headers Twilio might add
		if callSID == "" {
			for name, values := range r.Header {
				if strings.HasPrefix(strings.ToLower(name), "x-twilio") {
					log.Printf("Twilio header: %s = %v", name, values)
				}

				if (strings.ToLower(name) == "x-twilio-callsid" ||
					strings.ToLower(name) == "x-twilio-call-sid") && len(values) > 0 {
					callSID = values[0]
					log.Printf("Found CallSid %s in header %s", callSID, name)
				}
			}
		}

		// Try to parse form data if needed
		if callSID == "" {
			err := r.ParseForm()
			if err == nil {
				for name, values := range r.Form {
					log.Printf("Form field: %s = %v", name, values)

					if (name == "CallSid" || name == "callSid") && len(values) > 0 {
						callSID = values[0]
						log.Printf("Found CallSid %s in form field %s", callSID, name)
					}
				}
			}
		}

		// As a last resort, check for any active calls
		if callSID == "" {
			callSID = svc.ChannelManager.GetMostRecentCallSID()
			if callSID != "" {
				log.Printf("Using most recent call SID as fallback: %s", callSID)
			} else {
				log.Printf("WebSocket error: Could not determine CallSid from request")
				http.Error(w, "Missing CallSid parameter", http.StatusBadRequest)
				return
			}
		}

		log.Printf("Using CallSid: %s for WebSocket connection", callSID)

		// Upgrade the HTTP connection to a WebSocket connection
		log.Printf("Upgrading connection to WebSocket for call %s", callSID)
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("Error upgrading to WebSocket: %v", err)
			return
		}
		defer conn.Close()

		log.Printf("WebSocket connection established for call %s", callSID)

		// Get channels for this call
		channels, ok := svc.ChannelManager.GetChannels(callSID)
		if !ok {
			log.Printf("No channels found for call %s, creating new channels", callSID)
			channels = svc.ChannelManager.CreateChannels(callSID)
		}

		// Create conversation for this call
		conversation := svc.Conversation.GetOrCreateConversation(callSID)

		// Start processing audio for this call
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		log.Printf("Starting audio processing for call %s", callSID)
		svc.ChannelManager.StartAudioProcessing(ctx, callSID, svc.SpeechToText)

		// Process transcriptions and generate responses
		log.Printf("Starting transcription processing for call %s", callSID)
		go processTranscriptionsAndResponses(ctx, channels, conversation, svc)

		// Send audio responses back to the client
		log.Printf("Starting audio response sender for call %s", callSID)
		go sendAudioResponses(conn, channels)

		// Keep the connection alive and process messages
		for {
			// Read messages (might be JSON from Twilio)
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				log.Printf("WebSocket read error: %v", err)
				break
			}

			// Handle different message types
			switch messageType {
			case websocket.BinaryMessage:
				// Process binary audio data directly
				log.Printf("Received binary data: %d bytes", len(data))
				checkAudioFormat(data)
				channels.AppendAudioData(data)

			case websocket.TextMessage:
				// Parse text message as JSON
				log.Printf("Received text message: %s", string(data))

				var event TwilioWSEvent
				if err := json.Unmarshal(data, &event); err != nil {
					log.Printf("Error parsing JSON message: %v", err)
					continue
				}

				// Handle different event types
				switch event.Event {
				case "media":
					if event.Media == nil {
						log.Printf("Media event with no media data")
						continue
					}

					// Decode base64 payload
					mediaData, err := base64.StdEncoding.DecodeString(event.Media.Payload)
					if err != nil {
						log.Printf("Error decoding base64 payload: %v", err)
						continue
					}

					log.Printf("Decoded %d bytes of audio from media event", len(mediaData))
					checkAudioFormat(mediaData)
					channels.AppendAudioData(mediaData)

				case "start":
					log.Printf("Stream started: %s", event.StreamSid)

				case "stop":
					log.Printf("Stream stopped: %s", event.StreamSid)
					if event.Stop != nil {
						log.Printf("Call ended: %s", event.Stop.CallSid)
					}
					// Don't break the loop here, let the WebSocket close naturally

				default:
					log.Printf("Unknown event type: %s", event.Event)
				}

			case websocket.PingMessage:
				// Respond to pings with pongs
				log.Printf("Ping received, sending pong")
				if err := conn.WriteMessage(websocket.PongMessage, nil); err != nil {
					log.Printf("Error sending pong: %v", err)
				}

			default:
				log.Printf("Received message of type: %d", messageType)
			}
		}

		log.Printf("WebSocket connection closed for call %s", callSID)
	}
}

// Parse parameter string from Twilio header
func parseParameters(paramsString string) map[string]string {
	params := make(map[string]string)

	// Simple parameter parsing - can be extended for more complex formats
	parts := strings.Split(paramsString, ";")
	for _, part := range parts {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			key := strings.TrimSpace(kv[0])
			value := strings.TrimSpace(kv[1])
			params[key] = value
		}
	}

	return params
}

// Process transcriptions and generate responses
func processTranscriptionsAndResponses(
	ctx context.Context,
	channels *services.ChannelData,
	conversation *services.Conversation,
	svc *services.ServiceContainer,
) {
	log.Printf("Transcription processor started for call %s", channels.CallSID)

	// Add a ticker to periodically check if we're receiving transcriptions
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	lastActivity := time.Now()

	for {
		select {
		case <-ctx.Done():
			log.Printf("Transcription processor context done for call %s", channels.CallSID)
			return
		case <-ticker.C:
			idleTime := time.Since(lastActivity)
			log.Printf("Transcription processor idle for %v for call %s", idleTime, channels.CallSID)

			// Check channel buffer status to help diagnose issues
			log.Printf("Channel status for call %s: TranscriptionChan buffer has %d items, ResponseTextChan has %d items, ResponseAudioChan has %d items",
				channels.CallSID,
				len(channels.TranscriptionChan),
				len(channels.ResponseTextChan),
				len(channels.ResponseAudioChan))
		case transcription := <-channels.TranscriptionChan:
			lastActivity = time.Now()

			if transcription == "" {
				log.Printf("Empty transcription received for call %s, ignoring", channels.CallSID)
				continue
			}

			log.Printf("Transcription received for call %s: %q", channels.CallSID, transcription)

			// Add user message to conversation
			conversation.AddUserMessage(transcription)
			log.Printf("Added user message to conversation for call %s", channels.CallSID)

			// Get conversation history
			history := conversation.GetFormattedHistory()
			historyLength := len(history)
			log.Printf("Retrieved conversation history for call %s, %d messages", channels.CallSID, historyLength)

			// Generate AI response using Gemini
			log.Printf("Generating AI response for call %s with transcription: %q", channels.CallSID, transcription)
			startTime := time.Now()
			response, err := svc.Gemini.GenerateResponse(ctx, transcription, history)
			elapsed := time.Since(startTime)

			if err != nil {
				log.Printf("Error generating response for call %s: %v (after %v)", channels.CallSID, err, elapsed)
				// Send a fallback response in case of error
				response = "I'm sorry, I'm having trouble understanding right now. Could you please repeat that?"
			} else {
				log.Printf("AI response generated for call %s in %v: %q", channels.CallSID, elapsed, response)
			}

			// Add AI response to conversation
			conversation.AddTherapistMessage(response)
			log.Printf("Added therapist response to conversation for call %s", channels.CallSID)

			// Send the response text to the channel
			log.Printf("Sending text response to channel for call %s", channels.CallSID)
			select {
			case channels.ResponseTextChan <- response:
				log.Printf("Text response sent to channel for call %s", channels.CallSID)
			default:
				log.Printf("WARNING: ResponseTextChan is full for call %s, dropping message", channels.CallSID)
			}

			// Convert response to speech
			log.Printf("Converting response to speech for call %s: %q", channels.CallSID, response)
			startTime = time.Now()
			audioData, err := svc.TextToSpeech.SynthesizeSpeech(ctx, response)
			elapsed = time.Since(startTime)

			if err != nil {
				log.Printf("Error synthesizing speech for call %s: %v (after %v)", channels.CallSID, err, elapsed)
				continue
			}

			log.Printf("Text-to-speech conversion completed for call %s in %v, %d bytes",
				channels.CallSID, elapsed, len(audioData))

			// Send the audio to the channel
			log.Printf("Sending audio response to channel for call %s", channels.CallSID)
			select {
			case channels.ResponseAudioChan <- audioData:
				log.Printf("Audio response sent to channel for call %s", channels.CallSID)
			default:
				log.Printf("WARNING: ResponseAudioChan is full for call %s, dropping audio", channels.CallSID)
			}
		}
	}
}

// Send audio responses back to the client
func sendAudioResponses(conn *websocket.Conn, channels *services.ChannelData) {
	log.Printf("Audio response sender started for call %s", channels.CallSID)

	for {
		select {
		case audioData := <-channels.ResponseAudioChan:
			log.Printf("Sending audio data via WebSocket: %d bytes", len(audioData))

			// Send audio data to the client
			if err := conn.WriteMessage(websocket.BinaryMessage, audioData); err != nil {
				log.Printf("Error sending audio via WebSocket: %v", err)
				return
			}

			// Add a small delay to avoid flooding the connection
			time.Sleep(100 * time.Millisecond)
		}
	}
}
