package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ghophp/call-me-help/config"
	"github.com/ghophp/call-me-help/handlers"
	"github.com/ghophp/call-me-help/logger"
	"github.com/ghophp/call-me-help/services"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables
	err := godotenv.Load()
	if err != nil {
		// Using standard log before logger is initialized
		println("Warning: .env file not found")
	}

	// Load configuration
	cfg := config.Load()

	// Initialize logger with configured level
	logLevel := logger.INFO
	switch cfg.LogLevel {
	case "DEBUG":
		logLevel = logger.DEBUG
	case "INFO":
		logLevel = logger.INFO
	case "WARN":
		logLevel = logger.WARN
	case "ERROR":
		logLevel = logger.ERROR
	}
	logger.Initialize(logLevel)
	log := logger.GetDefaultLogger()
	log.Info("Starting Call-Me-Help application...")
	log.Info("Log level set to %s", cfg.LogLevel)

	// Parse command-line flags
	port := flag.String("port", cfg.Port, "server port")
	flag.Parse()

	log.Info("Initializing services...")

	// Initialize services
	ctx := context.Background()

	// Initialize Google Cloud clients
	log.Info("Initializing Speech-to-Text service...")
	speechClient, err := services.NewSpeechToTextService(ctx)
	if err != nil {
		log.Error("Failed to create Speech-to-Text client: %v", err)
		os.Exit(1)
	}
	defer speechClient.Close()

	log.Info("Initializing Text-to-Speech service...")
	ttsClient, err := services.NewTextToSpeechService(ctx)
	if err != nil {
		log.Error("Failed to create Text-to-Speech client: %v", err)
		os.Exit(1)
	}
	defer ttsClient.Close()

	log.Info("Initializing Gemini service...")
	geminiClient, err := services.NewGeminiService(ctx)
	if err != nil {
		log.Error("Failed to create Gemini client: %v", err)
		os.Exit(1)
	}
	defer geminiClient.Close()

	// Initialize conversation service for context management
	log.Info("Initializing Conversation service...")
	conversationService := services.NewConversationService()

	// Initialize channel manager
	log.Info("Initializing Channel Manager...")
	channelManager := services.NewChannelManager()

	// Initialize Twilio client
	log.Info("Initializing Twilio service...")
	twilioClient := services.NewTwilioService()

	// Create service container
	log.Info("Creating service container...")
	serviceContainer := &services.ServiceContainer{
		SpeechToText:   speechClient,
		TextToSpeech:   ttsClient,
		Gemini:         geminiClient,
		Twilio:         twilioClient,
		Conversation:   conversationService,
		ChannelManager: channelManager,
	}

	// Setup HTTP handlers
	log.Info("Setting up HTTP handlers...")
	mux := http.NewServeMux()

	mux.HandleFunc("POST /twilio/call", handlers.HandleIncomingCall(serviceContainer))
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
		log.Info("Server starting on port %s", *port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("Server error: %v", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal to gracefully shut down the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("Server shutting down...")

	// Create a deadline for server shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Error("Server forced to shutdown: %v", err)
		os.Exit(1)
	}

	log.Info("Server exited properly")
}
