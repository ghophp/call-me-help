package services

import (
	"context"

	"github.com/ghophp/call-me-help/config"
	"github.com/google/generative-ai-go/genai"
)

// GeminiService handles generation of AI responses using Google's Gemini
type GeminiService struct {
	client *genai.Client
	model  *genai.GenerativeModel
	config *config.Config
}

// NewGeminiService creates a new Gemini service
func NewGeminiService(ctx context.Context) (*GeminiService, error) {
	cfg := config.Load()

	// Create client using the default credentials from GOOGLE_APPLICATION_CREDENTIALS
	client, err := genai.NewClient(ctx)
	if err != nil {
		return nil, err
	}

	// Create a model instance
	model := client.GenerativeModel("gemini-1.5-pro")

	// Set temperature for more consistent responses
	model.SetTemperature(0.4)

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

	return &GeminiService{
		client: client,
		model:  model,
		config: cfg,
	}, nil
}

// Close closes the Gemini client
func (g *GeminiService) Close() error {
	g.client.Close()
	return nil
}

// GenerateResponse generates a therapeutic response based on user input and conversation history
func (g *GeminiService) GenerateResponse(ctx context.Context, userMessage string, conversationHistory []string) (string, error) {
	// Build the prompt with system instructions and conversation history
	prompt := `You are a professional psychotherapist providing helpful, empathetic advice to someone who needs mental health support.
Your responses should be supportive, non-judgmental, and focused on providing constructive guidance.
Always maintain a calm, compassionate tone. Prioritize the person's well-being and safety.
Never encourage harmful behaviors and suggest professional help when appropriate.
Keep responses concise and conversational - suitable for speaking in a phone call.
`

	// Add conversation history to build context
	for _, msg := range conversationHistory {
		prompt += "\n" + msg
	}

	// Add the current user message
	prompt += "\nUser: " + userMessage + "\nTherapist: "

	// Generate the response
	resp, err := g.model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		return "", err
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return "I'm sorry, I couldn't generate a response. Could you please rephrase your question?", nil
	}

	// Extract the text response
	response := resp.Candidates[0].Content.Parts[0].(genai.Text)
	return string(response), nil
}
