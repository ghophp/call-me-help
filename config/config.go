package config

import (
	"os"
	"strings"
)

// Config holds all configuration for the application
type Config struct {
	// Twilio Configuration
	TwilioAccountSID  string
	TwilioAuthToken   string
	TwilioPhoneNumber string

	// Google Cloud Configuration
	GoogleProjectID       string
	GoogleCredentialsPath string

	// Server Configuration
	Port string

	// Logging Configuration
	LogLevel string
}

// Load loads configuration from environment variables
func Load() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "INFO" // Default log level
	}
	logLevel = strings.ToUpper(logLevel)

	return &Config{
		TwilioAccountSID:      os.Getenv("TWILIO_ACCOUNT_SID"),
		TwilioAuthToken:       os.Getenv("TWILIO_AUTH_TOKEN"),
		TwilioPhoneNumber:     os.Getenv("TWILIO_PHONE_NUMBER"),
		GoogleProjectID:       os.Getenv("GOOGLE_PROJECT_ID"),
		GoogleCredentialsPath: os.Getenv("GOOGLE_APPLICATION_CREDENTIALS"),
		Port:                  port,
		LogLevel:              logLevel,
	}
}
