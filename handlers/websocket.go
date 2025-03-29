package handlers

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/ghophp/call-me-help/services"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for this demo
	},
}

// HandleWebSocket handles WebSocket connections for streaming audio
func HandleWebSocket(svc *services.ServiceContainer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get call ID from query parameters
		callSID := r.URL.Query().Get("callSid")
		if callSID == "" {
			http.Error(w, "Missing callSid parameter", http.StatusBadRequest)
			return
		}

		// Upgrade the HTTP connection to a WebSocket connection
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
			log.Printf("No channels found for call %s", callSID)
			return
		}

		// Create conversation for this call
		conversation := svc.Conversation.GetOrCreateConversation(callSID)

		// Start processing audio for this call
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		svc.ChannelManager.StartAudioProcessing(ctx, callSID, svc.SpeechToText)

		// Process transcriptions and generate responses
		go processTranscriptionsAndResponses(ctx, channels, conversation, svc)

		// Send audio responses back to the client
		go sendAudioResponses(conn, channels)

		// Keep the connection alive
		for {
			// Read messages (might be control messages from client)
			_, _, err := conn.ReadMessage()
			if err != nil {
				log.Printf("WebSocket read error: %v", err)
				break
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
	for {
		select {
		case <-ctx.Done():
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
			audioData, err := svc.TextToSpeech.SynthesizeSpeech(ctx, response)
			if err != nil {
				log.Printf("Error synthesizing speech: %v", err)
				continue
			}

			// Send the audio to the channel
			channels.ResponseAudioChan <- audioData
		}
	}
}

// Send audio responses back to the client
func sendAudioResponses(conn *websocket.Conn, channels *services.ChannelData) {
	for {
		select {
		case audioData := <-channels.ResponseAudioChan:
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
