package services

import (
	"context"
	"os"
	"time"

	"github.com/ghophp/call-me-help/config"
	"github.com/ghophp/call-me-help/logger"
	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

// GeminiService handles generation of AI responses using Google's Gemini
type GeminiService struct {
	client *genai.Client
	model  *genai.GenerativeModel
	config *config.Config
	log    *logger.Logger
}

// NewGeminiService creates a new Gemini service
func NewGeminiService(ctx context.Context) (*GeminiService, error) {
	cfg := config.Load()
	log := logger.Component("Gemini")

	log.Info("Creating new Gemini service")

	// Check for API key in environment variable
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		log.Warn("GEMINI_API_KEY environment variable not set, will try to use service account credentials")
	} else {
		log.Debug("Found GEMINI_API_KEY in environment variables")
	}

	// Create client using API key if available, otherwise default credentials
	var client *genai.Client
	var err error

	if apiKey != "" {
		// Use API key authentication
		client, err = genai.NewClient(ctx, option.WithAPIKey(apiKey))
		if err != nil {
			log.Error("Error creating Gemini client with API key: %v", err)
			return nil, err
		}
		log.Info("Gemini client created successfully using API key")
	} else {
		// Fall back to default credentials if no API key is provided
		client, err = genai.NewClient(ctx)
		if err != nil {
			log.Error("Error creating Gemini client with default credentials: %v", err)
			return nil, err
		}
		log.Info("Gemini client created successfully using default credentials")
	}

	// Create a model instance
	model := client.GenerativeModel("gemini-1.5-pro")
	log.Info("Using Gemini model: gemini-1.5-pro")

	// Set temperature for more consistent responses
	model.SetTemperature(0.4)
	log.Debug("Set Gemini temperature to 0.4")

	// Configure safety settings for therapeutic context
	model.SafetySettings = []*genai.SafetySetting{
		{
			Category:  genai.HarmCategoryHarassment,
			Threshold: genai.HarmBlockThreshold(2), // Medium threshold
		},
		{
			Category:  genai.HarmCategoryHateSpeech,
			Threshold: genai.HarmBlockThreshold(2), // Medium threshold
		},
		{
			Category:  genai.HarmCategorySexuallyExplicit,
			Threshold: genai.HarmBlockThreshold(2), // Medium threshold
		},
		{
			Category:  genai.HarmCategoryDangerousContent,
			Threshold: genai.HarmBlockThreshold(2), // Medium threshold
		},
	}
	log.Debug("Configured Gemini safety settings with medium threshold (2)")

	return &GeminiService{
		client: client,
		model:  model,
		config: cfg,
		log:    log,
	}, nil
}

// Close closes the Gemini client
func (g *GeminiService) Close() error {
	g.log.Info("Closing Gemini client")
	g.client.Close()
	return nil
}

// GenerateResponse generates a therapeutic response based on user input and conversation history
func (g *GeminiService) GenerateResponse(ctx context.Context, userMessage string, conversationHistory []string) (string, error) {
	startTime := time.Now()
	g.log.Info("Generating response for message: %q", userMessage)

	// Build the prompt with system instructions and conversation history
	prompt := `You are a professional psychotherapist providing helpful, empathetic advice to someone who needs mental health support.
Your responses should be supportive, non-judgmental, and focused on providing constructive guidance.
Always maintain a calm, compassionate tone. Prioritize the person's well-being and safety.
Never encourage harmful behaviors and suggest professional help when appropriate.
Keep responses concise and conversational - suitable for speaking in a phone call.
`

	// Add conversation history to build context
	promptWithHistory := prompt
	for i, msg := range conversationHistory {
		promptWithHistory += "\n" + msg
		if i < len(conversationHistory)-5 {
			// Only log the most recent 5 messages to avoid very long logs
			continue
		}
		g.log.Debug("History[%d]: %s", i, msg)
	}

	// Add the current user message
	promptWithHistory += "\nUser: " + userMessage + "\nTherapist: "

	g.log.Debug("Built prompt with %d conversation history messages", len(conversationHistory))

	// Create a timeout for the API call
	genCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Generate the response
	g.log.Debug("Calling Gemini API...")
	resp, err := g.model.GenerateContent(genCtx, genai.Text(promptWithHistory))
	callDuration := time.Since(startTime)

	if err != nil {
		g.log.Error("Gemini API error after %v: %v", callDuration, err)
		return "", err
	}

	g.log.Debug("Gemini API call completed in %v", callDuration)

	if len(resp.Candidates) == 0 {
		g.log.Warn("Gemini returned no candidates")
		return "I'm sorry, I couldn't generate a response. Could you please rephrase your question?", nil
	}

	g.log.Debug("Gemini returned %d candidates", len(resp.Candidates))

	if len(resp.Candidates[0].Content.Parts) == 0 {
		g.log.Warn("Gemini returned empty content parts")
		return "I'm sorry, I couldn't generate a response. Could you please rephrase your question?", nil
	}

	// Extract the text response
	response := resp.Candidates[0].Content.Parts[0].(genai.Text)
	responseStr := string(response)
	g.log.Info("Gemini response (%d chars): %q", len(responseStr), responseStr)

	totalDuration := time.Since(startTime)
	g.log.Debug("Total response generation completed in %v", totalDuration)

	return responseStr, nil
}
