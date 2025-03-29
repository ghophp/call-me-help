package handlers

import (
	"context"
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

		// Keep the connection alive and process binary audio data
		for {
			// Read messages (audio data or control messages)
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				log.Printf("WebSocket read error: %v", err)
				break
			}

			// Handle different message types
			switch messageType {
			case websocket.BinaryMessage:
				// Process binary audio data
				log.Printf("Received binary data: %d bytes", len(data))
				channels.AppendAudioData(data)

			case websocket.TextMessage:
				// Log text messages for debugging
				log.Printf("Received text message: %s", string(data))

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

	for {
		select {
		case <-ctx.Done():
			log.Printf("Transcription processor context done for call %s", channels.CallSID)
			return
		case transcription := <-channels.TranscriptionChan:
			if transcription == "" {
				continue
			}

			log.Printf("Transcription received: %s", transcription)

			// Add user message to conversation
			conversation.AddUserMessage(transcription)

			// Get conversation history
			history := conversation.GetFormattedHistory()

			// Generate AI response using Gemini
			log.Printf("Generating AI response for: %s", transcription)
			response, err := svc.Gemini.GenerateResponse(ctx, transcription, history)
			if err != nil {
				log.Printf("Error generating response: %v", err)
				continue
			}

			// Add AI response to conversation
			conversation.AddTherapistMessage(response)
			log.Printf("AI response generated: %s", response)

			// Send the response text to the channel
			channels.ResponseTextChan <- response

			// Convert response to speech
			log.Printf("Converting response to speech: %s", response)
			audioData, err := svc.TextToSpeech.SynthesizeSpeech(ctx, response)
			if err != nil {
				log.Printf("Error synthesizing speech: %v", err)
				continue
			}

			log.Printf("Text-to-speech conversion completed, %d bytes", len(audioData))

			// Send the audio to the channel
			channels.ResponseAudioChan <- audioData
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
