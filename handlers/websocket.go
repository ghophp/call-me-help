package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
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

		// Store stream SID for later use
		streamSID := "STREAM_" + callSID
		var streamMutex sync.Mutex
		updateStreamSID := func(sid string) {
			streamMutex.Lock()
			defer streamMutex.Unlock()
			if sid != "" {
				streamSID = sid
				log.Info("Updated StreamSid to: %s", streamSID)
			}
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

		// Send a "mark" event immediately to confirm connection and align with protocol
		// Needs streamSid, which might not be the final one yet, but Twilio expects it.
		streamMutex.Lock()
		initialStreamSID := streamSID // Use the placeholder SID initially
		streamMutex.Unlock()
		markMsg := map[string]interface{}{ // Use interface{} for nested map
			"event":     "mark",
			"streamSid": initialStreamSID,
			"mark": map[string]string{
				"name": "connection_established",
			},
		}
		if err := conn.WriteJSON(markMsg); err != nil {
			log.Error("Error sending initial mark event: %v", err)
		} else {
			log.Info("Sent initial mark event to confirm connection")
		}

		// Get channels for this call
		channels, ok := svc.ChannelManager.GetChannels(callSID)
		if !ok {
			log.Info("No channels found for call %s, creating new channels", callSID)
			channels = svc.ChannelManager.CreateChannels(callSID)
		}

		// Send a simple welcome message
		go func() {
			// Wait a brief moment to ensure everything is set up
			time.Sleep(2 * time.Second)

			// Send welcome message
			welcomeMsg := "Hello. I'm your AI therapist. How are you feeling today?"
			log.Info("Sending welcome message: %s", welcomeMsg)

			select {
			case channels.ResponseTextChan <- welcomeMsg:
				log.Info("Welcome message sent to text channel")
			default:
				log.Warn("Could not send welcome message, text channel full")
			}
		}()

		// Create conversation for this call
		conversation := svc.Conversation.GetOrCreateConversation(callSID)

		// Add a new context value to pass the streamSID
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ctx = context.WithValue(ctx, "streamSID", streamSID)

		// Start processing audio for this call
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
		go sendAudioResponses(conn, channels, &streamSID, &streamMutex, log)

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
		go func(currentConn *websocket.Conn, sidMutex *sync.Mutex) {
			ticker := time.NewTicker(15 * time.Second) // More frequent pings
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					log.Debug("Sending ping to client")
					if err := currentConn.WriteControl(websocket.PingMessage, []byte("keepalive"), time.Now().Add(10*time.Second)); err != nil {
						log.Error("Error sending ping: %v", err)
						// Don't return on error, try to keep the connection alive
						continue
					}

					// Also send a keepalive mark message with the correct stream SID
					sidMutex.Lock()
					currentKeepaliveStreamSID := streamSID
					sidMutex.Unlock()
					keepaliveMarkMsg := map[string]interface{}{ // Use interface{} for nested map
						"event":     "mark",
						"streamSid": currentKeepaliveStreamSID,
						"mark": map[string]string{
							"name": "keepalive_" + strconv.FormatInt(time.Now().Unix(), 10),
						},
					}
					if err := currentConn.WriteJSON(keepaliveMarkMsg); err != nil {
						log.Error("Error sending keepalive mark: %v", err)
					}
				}
			}
		}(conn, &streamMutex)

		// Keep the connection alive and process messages
		for {
			// Set a longer read deadline to prevent timeouts
			if err := conn.SetReadDeadline(time.Time{}); err != nil {
				log.Error("Error setting read deadline: %v", err)
				// Continue anyway
			}

			// Read messages (might be JSON from Twilio)
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Error("WebSocket unexpected close error: %v", err)
				} else {
					log.Info("WebSocket connection closed: %v", err)
				}
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
						log.Warn("Media event with no media data for call %s", callSID)
						continue
					}

					// Decode base64 payload to binary
					decodedPayload, err := base64.StdEncoding.DecodeString(event.Media.Payload)
					if err != nil {
						log.Error("Error decoding base64 payload: %v", err)
						continue
					}

					log.Debug("Decoded %d bytes of audio data from track: %s", len(decodedPayload), event.Media.Track)

					// Send to speech recognition
					err = stream.Send(&speechpb.StreamingRecognizeRequest{
						StreamingRequest: &speechpb.StreamingRecognizeRequest_AudioContent{
							AudioContent: decodedPayload,
						},
					})

					if err != nil {
						log.Error("Error sending audio to speech recognition: %v", err)
					} else {
						log.Debug("Sent %d bytes to speech recognition", len(decodedPayload))
					}

				case "start":
					log.Info("Stream started: %s for call %s", event.StreamSid, callSID)

					// Update the StreamSid with the actual one from Twilio
					updateStreamSID(event.StreamSid)

					// Send a welcome message
					welcomeMsg := "Connection established. I'm listening."
					select {
					case channels.ResponseTextChan <- welcomeMsg:
						log.Debug("Sent welcome message to response channel")
					default:
						log.Warn("Could not send welcome message, channel full")
					}

				case "stop":
					log.Info("Stream stopped: %s", event.StreamSid)
					if event.Stop != nil {
						log.Info("Call ended: %s", event.Stop.CallSid)
					}

				case "mark":
					log.Debug("Mark event received: %v", event)

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
				log.Debug("Received message of type: %d with %d bytes", messageType, len(data))
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

	// Save the TTS-generated audio to a file
	if err := svc.TextToSpeech.SaveAudioToFile(channels.CallSID, response, audioData); err != nil {
		log.Error("Error saving TTS audio to file for call %s: %v", channels.CallSID, err)
		// Continue even if saving fails - this is a non-critical operation
	}

	// Send the audio to the channel FOR the sendAudioResponses goroutine to handle
	log.Info("Sending audio response to channel for call %s", channels.CallSID)
	select {
	case channels.ResponseAudioChan <- audioData:
		log.Debug("Audio response sent to channel for call %s", channels.CallSID)
	default:
		log.Warn("ResponseAudioChan is full for call %s, dropping audio", channels.CallSID)
	}
}

// Send audio responses back to the client
// Accept pointer to streamSID
func sendAudioResponses(conn *websocket.Conn, channels *services.ChannelData, streamSID *string, streamMutex *sync.Mutex, log *logger.Logger) {
	log.Info("Audio response sender started for call %s", channels.CallSID)

	// Maximum chunk size to avoid large packets - keep under 16KB
	const maxChunkSize = 3200 // 400ms of 8kHz audio (μ-law is 8000 samples/sec at 8-bit)

	// Send media message in Twilio format
	sendMediaMessage := func(data []byte) error {
		// Get the current streamSID (could have been updated)
		streamMutex.Lock()
		// Read the shared streamSID via the pointer
		currentMediaStreamSID := *streamSID
		streamMutex.Unlock()

		// Get payload details
		encodedData := base64.StdEncoding.EncodeToString(data)

		log.Info("Preparing to send audio chunk")

		// Construct media message according to Twilio docs for OUTBOUND playback
		// https://www.twilio.com/docs/voice/twiml/stream#message-media-playback
		mediaMsg := map[string]interface{}{ // Use interface{} to allow nested map
			"event":     "media",
			"streamSid": currentMediaStreamSID, // Use locally read SID
			"media": map[string]string{
				"payload": encodedData,
				// DO NOT include track, chunk, or timestamp for outbound playback messages
			},
		}

		// Marshal to JSON
		jsonBytes, err := json.Marshal(mediaMsg)
		if err != nil {
			log.Error("Error marshaling media message: %v", err)
			return err
		}

		// Send the message
		log.Info("Sending audio chunk of %d bytes", len(data))
		return conn.WriteMessage(websocket.TextMessage, jsonBytes)
	}

	for {
		select {
		case audioData, ok := <-channels.ResponseAudioChan:
			if !ok {
				log.Warn("Audio response channel closed for call %s", channels.CallSID)
				return
			}

			log.Info("Sending audio data via WebSocket for call %s: %d bytes", channels.CallSID, len(audioData))

			// For large audio files, break them into smaller chunks
			if len(audioData) > maxChunkSize {
				log.Debug("Breaking audio into chunks for call %s, total size: %d bytes",
					channels.CallSID, len(audioData))

				totalChunks := (len(audioData) + maxChunkSize - 1) / maxChunkSize
				log.Info("Will send %d audio chunks for call %s", totalChunks, channels.CallSID)

				for i := 0; i < totalChunks; i++ {
					start := i * maxChunkSize
					end := start + maxChunkSize
					if end > len(audioData) {
						end = len(audioData)
					}

					chunk := audioData[start:end]
					log.Info("Sending chunk %d/%d of size %d bytes for call %s",
						i+1, totalChunks, len(chunk), channels.CallSID)

					// Send in Twilio's expected format
					if err := sendMediaMessage(chunk); err != nil {
						log.Error("Error sending audio chunk %d/%d: %v", i+1, totalChunks, err)
						// Try to continue with next chunk rather than breaking
						continue
					}

					// Add a moderate delay between chunks
					time.Sleep(100 * time.Millisecond)
				}

				log.Info("Finished sending all %d chunks for call %s", totalChunks, channels.CallSID)
			} else {
				// For small audio files, just send them directly
				if err := sendMediaMessage(audioData); err != nil {
					log.Error("Error sending audio via WebSocket: %v", err)
					continue
				}
			}

			// Add a larger delay after sending audio to ensure Twilio processes it
			time.Sleep(200 * time.Millisecond)
		}
	}
}
