package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/ghophp/call-me-help/logger"
	"github.com/ghophp/call-me-help/services"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for Twilio connections
		logger.Debug("WebSocket origin check: %s", r.Header.Get("Origin"))
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

// TranscriptionBuffer collects and normalizes transcriptions
type TranscriptionBuffer struct {
	LastActivity    time.Time
	Transcriptions  []string
	LastTranscript  string
	ProcessingSince time.Time
	IsProcessing    bool
}

// NewTranscriptionBuffer creates a new transcription buffer
func NewTranscriptionBuffer() *TranscriptionBuffer {
	return &TranscriptionBuffer{
		LastActivity:   time.Now(),
		Transcriptions: make([]string, 0),
	}
}

// AddTranscription adds a transcription to the buffer
func (tb *TranscriptionBuffer) AddTranscription(transcription string) {
	tb.LastActivity = time.Now()
	tb.Transcriptions = append(tb.Transcriptions, transcription)
	tb.LastTranscript = transcription
}

// ShouldProcess determines if the buffer should be processed based on silence duration
func (tb *TranscriptionBuffer) ShouldProcess(silenceDuration time.Duration) bool {
	return !tb.IsProcessing &&
		len(tb.Transcriptions) > 0 &&
		time.Since(tb.LastActivity) > silenceDuration
}

// StartProcessing marks the buffer as being processed
func (tb *TranscriptionBuffer) StartProcessing() {
	tb.ProcessingSince = time.Now()
	tb.IsProcessing = true
}

// FinishProcessing resets the buffer after processing
func (tb *TranscriptionBuffer) FinishProcessing() {
	tb.Transcriptions = make([]string, 0)
	tb.IsProcessing = false
}

// NormalizeTranscriptions processes the transcriptions to find the most complete one
func (tb *TranscriptionBuffer) NormalizeTranscriptions() string {
	if len(tb.Transcriptions) == 0 {
		return ""
	}

	// Use the last transcription, which is likely the most complete
	finalTranscription := tb.Transcriptions[len(tb.Transcriptions)-1]

	// Clean up extra spaces
	finalTranscription = strings.TrimSpace(finalTranscription)

	return finalTranscription
}

// HandleWebSocket handles WebSocket connections for streaming audio
func HandleWebSocket(svc *services.ServiceContainer) http.HandlerFunc {
	log := logger.Component("WebSocket")

	return func(w http.ResponseWriter, r *http.Request) {
		log.Info("WebSocket connection request received: %s", r.URL.String())

		callSID := svc.ChannelManager.GetMostRecentCallSID()
		if callSID != "" {
			log.Info("Using most recent call SID as fallback: %s", callSID)
		} else {
			log.Error("WebSocket error: Could not determine CallSid from request")
			http.Error(w, "Missing CallSid parameter", http.StatusBadRequest)
			return
		}

		log.Info("Using CallSid: %s for WebSocket connection", callSID)

		// Upgrade the HTTP connection to a WebSocket connection
		log.Info("Upgrading connection to WebSocket for call %s", callSID)
		upgrader.CheckOrigin = func(r *http.Request) bool {
			// Log origin for debugging
			origin := r.Header.Get("Origin")
			log.Debug("WebSocket origin check: %s", origin)
			return true // Accept all origins for Twilio
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Error("Error upgrading to WebSocket: %v", err)
			return
		}
		defer conn.Close()

		// Set a longer read deadline to prevent timeouts
		conn.SetReadDeadline(time.Time{}) // No deadline
		log.Info("WebSocket connection established for call %s", callSID)

		// Get channels for this call
		channels, ok := svc.ChannelManager.GetChannels(callSID)
		if !ok {
			log.Info("No channels found for call %s, creating new channels", callSID)
			channels = svc.ChannelManager.CreateChannels(callSID)
		}

		// Create conversation for this call
		conversation := svc.Conversation.GetOrCreateConversation(callSID)

		// Start processing audio for this call
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		log.Info("Starting audio processing for call %s", callSID)
		stream, err := svc.ChannelManager.StartAudioProcessing(ctx, callSID, svc.SpeechToText)
		if err != nil {
			log.Error("Error starting audio processing for call %s: %v", callSID, err)
			return
		}

		// Process transcriptions and generate responses
		log.Info("Starting transcription processing for call %s", callSID)
		go processTranscriptionsAndResponses(ctx, channels, conversation, svc, log)

		// Send audio responses back to the client
		log.Info("Starting audio response sender for call %s", callSID)
		go sendAudioResponses(conn, channels, log)

		// Add a ping handler
		conn.SetPingHandler(func(data string) error {
			log.Debug("Received ping from client, sending pong")
			err := conn.WriteControl(websocket.PongMessage, []byte{}, time.Now().Add(time.Second))
			if err != nil {
				log.Error("Error sending pong: %v", err)
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
					log.Debug("Sending ping to client")
					if err := conn.WriteControl(websocket.PingMessage, []byte{}, time.Now().Add(time.Second)); err != nil {
						log.Error("Error sending ping: %v", err)
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
				log.Error("WebSocket read error: %v", err)
				break
			}

			// Handle different message types
			switch messageType {
			case websocket.TextMessage:
				// Parse text message as JSON
				log.Debug("Received text message: %s", string(data))

				var event TwilioWSEvent
				if err := json.Unmarshal(data, &event); err != nil {
					log.Error("Error parsing JSON message: %v", err)
					continue
				}

				// Handle different event types
				switch event.Event {
				case "media":
					if event.Media == nil {
						log.Warn("Media event with no media data")
						continue
					}

					// Decode base64 payload to binary
					decodedPayload, err := base64.StdEncoding.DecodeString(event.Media.Payload)
					if err != nil {
						log.Error("Error decoding base64 payload: %v", err)
						continue
					}

					stream.Send(&speechpb.StreamingRecognizeRequest{
						StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
							AudioContent: decodedPayload,
						},
					})
				case "start":
					log.Info("Stream started: %s", event.StreamSid)
				case "stop":
					log.Info("Stream stopped: %s", event.StreamSid)
					if event.Stop != nil {
						log.Info("Call ended: %s", event.Stop.CallSid)
					}
					// Don't terminate connection here - let client close it
					// so we can continue processing buffered audio

				default:
					log.Warn("Unknown event type: %s", event.Event)
				}

			case websocket.PingMessage:
				// Respond to pings with pongs
				log.Debug("Ping received, sending pong")
				if err := conn.WriteMessage(websocket.PongMessage, nil); err != nil {
					log.Error("Error sending pong: %v", err)
				}

			default:
				log.Debug("Received message of type: %d", messageType)
			}
		}

		log.Info("WebSocket connection closed for call %s", callSID)
	}
}

// Process transcriptions and generate responses
func processTranscriptionsAndResponses(
	ctx context.Context,
	channels *services.ChannelData,
	conversation *services.Conversation,
	svc *services.ServiceContainer,
	log *logger.Logger,
) {
	log.Info("Transcription processor started for call %s", channels.CallSID)

	// Add a ticker to periodically check if we're receiving transcriptions
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Create a transcription buffer
	buffer := NewTranscriptionBuffer()

	// Configure silence detection
	silenceDuration := 2 * time.Second
	log.Info("Silence detection configured for %v", silenceDuration)

	for {
		select {
		case <-ctx.Done():
			log.Info("Transcription processor context done for call %s", channels.CallSID)
			return
		case <-ticker.C:
			// Check if we should process the buffer
			if buffer.ShouldProcess(silenceDuration) {
				silenceTime := time.Since(buffer.LastActivity)
				log.Info("Detected %v silence, processing transcriptions for call %s", silenceTime, channels.CallSID)

				// Mark as processing to avoid concurrent processing
				buffer.StartProcessing()

				// Normalize transcriptions
				normalized := buffer.NormalizeTranscriptions()
				log.Info("Normalized transcription for call %s: %q", channels.CallSID, normalized)

				if normalized != "" {
					// Process the normalized transcription
					processTranscription(ctx, normalized, channels, conversation, svc, log)
				}

				// Reset buffer
				buffer.FinishProcessing()
			}

			// Periodically log status
			if time.Since(buffer.LastActivity) > 10*time.Second && len(buffer.Transcriptions) > 0 {
				log.Debug("Transcription buffer status: %d items, last activity %v ago",
					len(buffer.Transcriptions), time.Since(buffer.LastActivity))
			}

		case transcription := <-channels.TranscriptionChan:
			if transcription == "" {
				log.Debug("Empty transcription received for call %s, ignoring", channels.CallSID)
				continue
			}

			log.Debug("Transcription received for call %s: %q", channels.CallSID, transcription)
			buffer.AddTranscription(transcription)
		}
	}
}

// Process a single normalized transcription
func processTranscription(
	ctx context.Context,
	transcription string,
	channels *services.ChannelData,
	conversation *services.Conversation,
	svc *services.ServiceContainer,
	log *logger.Logger,
) {
	// Add user message to conversation
	conversation.AddUserMessage(transcription)
	log.Info("Added user message to conversation for call %s: %q", channels.CallSID, transcription)

	// Get conversation history
	history := conversation.GetFormattedHistory()
	historyLength := len(history)
	log.Debug("Retrieved conversation history for call %s, %d messages", channels.CallSID, historyLength)

	// Generate AI response using Gemini
	log.Info("Generating AI response for call %s", channels.CallSID)
	startTime := time.Now()
	response, err := svc.Gemini.GenerateResponse(ctx, transcription, history)
	elapsed := time.Since(startTime)

	if err != nil {
		log.Error("Error generating response for call %s: %v (after %v)", channels.CallSID, err, elapsed)
		// Send a fallback response in case of error
		response = "I'm sorry, I'm having trouble understanding right now. Could you please repeat that?"
	} else {
		log.Info("AI response generated for call %s in %v", channels.CallSID, elapsed)
	}

	// Add AI response to conversation
	conversation.AddTherapistMessage(response)
	log.Info("Added therapist response to conversation for call %s", channels.CallSID)

	// Send the response text to the channel
	log.Debug("Sending text response to channel for call %s", channels.CallSID)
	select {
	case channels.ResponseTextChan <- response:
		log.Debug("Text response sent to channel for call %s", channels.CallSID)
	default:
		log.Warn("ResponseTextChan is full for call %s, dropping message", channels.CallSID)
	}

	// Convert response to speech
	log.Info("Converting response to speech for call %s", channels.CallSID)
	startTime = time.Now()
	audioData, err := svc.TextToSpeech.SynthesizeSpeech(ctx, response)
	elapsed = time.Since(startTime)

	if err != nil {
		log.Error("Error synthesizing speech for call %s: %v (after %v)", channels.CallSID, err, elapsed)
		return
	}

	log.Info("Text-to-speech conversion completed for call %s in %v, %d bytes",
		channels.CallSID, elapsed, len(audioData))

	// Send the audio to the channel
	log.Debug("Sending audio response to channel for call %s", channels.CallSID)
	select {
	case channels.ResponseAudioChan <- audioData:
		log.Debug("Audio response sent to channel for call %s", channels.CallSID)
	default:
		log.Warn("ResponseAudioChan is full for call %s, dropping audio", channels.CallSID)
	}
}

// Send audio responses back to the client
func sendAudioResponses(conn *websocket.Conn, channels *services.ChannelData, log *logger.Logger) {
	log.Info("Audio response sender started for call %s", channels.CallSID)

	for {
		select {
		case audioData := <-channels.ResponseAudioChan:
			log.Info("Sending audio data via WebSocket: %d bytes", len(audioData))

			// Send audio data to the client
			if err := conn.WriteMessage(websocket.BinaryMessage, audioData); err != nil {
				log.Error("Error sending audio via WebSocket: %v", err)
				return
			}

			// Add a small delay to avoid flooding the connection
			time.Sleep(100 * time.Millisecond)
		}
	}
}
