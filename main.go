package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ghophp/call-me-help/handlers"
	"github.com/ghophp/call-me-help/services"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: .env file not found")
	}

	// Parse command-line flags
	port := flag.String("port", "8080", "server port")
	flag.Parse()

	// Initialize services
	ctx := context.Background()

	// Initialize Google Cloud clients
	speechClient, err := services.NewSpeechToTextService(ctx)
	if err != nil {
		log.Fatalf("Failed to create Speech-to-Text client: %v", err)
	}
	defer speechClient.Close()

	ttsClient, err := services.NewTextToSpeechService(ctx)
	if err != nil {
		log.Fatalf("Failed to create Text-to-Speech client: %v", err)
	}
	defer ttsClient.Close()

	geminiClient, err := services.NewGeminiService(ctx)
	if err != nil {
		log.Fatalf("Failed to create Gemini client: %v", err)
	}
	defer geminiClient.Close()

	// Initialize conversation service for context management
	conversationService := services.NewConversationService()

	// Initialize channel manager
	channelManager := services.NewChannelManager()

	// Initialize Twilio client
	twilioClient := services.NewTwilioService()

	// Create service container
	serviceContainer := &services.ServiceContainer{
		SpeechToText:   speechClient,
		TextToSpeech:   ttsClient,
		Gemini:         geminiClient,
		Twilio:         twilioClient,
		Conversation:   conversationService,
		ChannelManager: channelManager,
	}

	// Setup HTTP handlers
	mux := http.NewServeMux()

	// Twilio webhook endpoints
	mux.HandleFunc("POST /twilio/call", handlers.HandleIncomingCall(serviceContainer))
	mux.HandleFunc("POST /twilio/stream", handlers.HandleTwilioStream(serviceContainer))

	// WebSocket endpoint for media streaming
	mux.HandleFunc("GET /ws", handlers.HandleWebSocket(serviceContainer))

	// Health check endpoint
	mux.HandleFunc("GET /health", handlers.HealthCheck)

	// Create the HTTP server
	server := &http.Server{
		Addr:    ":" + *port,
		Handler: mux,
	}

	// Start the server in a goroutine
	go func() {
		log.Printf("Server starting on port %s", *port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shut down the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Server shutting down...")

	// Create a deadline for server shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited properly")
}
