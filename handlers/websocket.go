package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"cloud.google.com/go/speech/apiv1/speechpb"
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

// HandleWebSocket handles WebSocket connections for streaming audio
func HandleWebSocket(svc *services.ServiceContainer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Printf("WebSocket connection request received: %s", r.URL.String())

		callSID := svc.ChannelManager.GetMostRecentCallSID()
		if callSID != "" {
			log.Printf("Using most recent call SID as fallback: %s", callSID)
		} else {
			log.Printf("WebSocket error: Could not determine CallSid from request")
			http.Error(w, "Missing CallSid parameter", http.StatusBadRequest)
			return
		}

		log.Printf("Using CallSid: %s for WebSocket connection", callSID)

		// Upgrade the HTTP connection to a WebSocket connection
		log.Printf("Upgrading connection to WebSocket for call %s", callSID)
		upgrader.CheckOrigin = func(r *http.Request) bool {
			// Log origin for debugging
			origin := r.Header.Get("Origin")
			log.Printf("WebSocket origin check: %s", origin)
			return true // Accept all origins for Twilio
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("Error upgrading to WebSocket: %v", err)
			return
		}
		defer conn.Close()

		// Set a longer read deadline to prevent timeouts
		conn.SetReadDeadline(time.Time{}) // No deadline
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
		stream, err := svc.ChannelManager.StartAudioProcessing(ctx, callSID, svc.SpeechToText)
		if err != nil {
			log.Printf("Error starting audio processing for call %s: %v", callSID, err)
			return
		}

		// Process transcriptions and generate responses
		log.Printf("Starting transcription processing for call %s", callSID)
		go processTranscriptionsAndResponses(ctx, channels, conversation, svc)

		// Send audio responses back to the client
		log.Printf("Starting audio response sender for call %s", callSID)
		go sendAudioResponses(conn, channels)

		// Add a ping handler
		conn.SetPingHandler(func(data string) error {
			log.Printf("Received ping from client, sending pong")
			err := conn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(time.Second))
			if err != nil {
				log.Printf("Error sending pong: %v", err)
			}
			return nil
		})

		// Keep the connection alive with pings
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					log.Printf("Sending ping to client")
					if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(time.Second)); err != nil {
						log.Printf("Error sending ping: %v", err)
						return
					}
				}
			}
		}()

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

					// Decode base64 payload to binary
					decodedPayload, err := base64.StdEncoding.DecodeString(event.Media.Payload)
					if err != nil {
						log.Printf("Error decoding base64 payload: %v", err)
						continue
					}

					stream.Send(&speechpb.StreamingRecognizeRequest{
						StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
							AudioContent: decodedPayload,
						},
					})
				case "start":
					log.Printf("Stream started: %s", event.StreamSid)
				case "stop":
					log.Printf("Stream stopped: %s", event.StreamSid)
					if event.Stop != nil {
						log.Printf("Call ended: %s", event.Stop.CallSid)
					}
					// Don't terminate connection here - let client close it
					// so we can continue processing buffered audio

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
