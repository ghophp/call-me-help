package services

import (
	"sync"
)

// Message represents a message in the conversation
type Message struct {
	Role    string // "user" or "therapist"
	Content string
}

// Conversation represents a therapy conversation
type Conversation struct {
	ID       string
	Messages []Message
	mu       sync.Mutex
}

// ConversationService manages conversation history
type ConversationService struct {
	conversations map[string]*Conversation
	mu            sync.Mutex
}

// NewConversationService creates a new conversation service
func NewConversationService() *ConversationService {
	return &ConversationService{
		conversations: make(map[string]*Conversation),
	}
}

// GetOrCreateConversation gets or creates a conversation by ID
func (c *ConversationService) GetOrCreateConversation(id string) *Conversation {
	c.mu.Lock()
	defer c.mu.Unlock()

	if conv, ok := c.conversations[id]; ok {
		return conv
	}

	// Create a new conversation
	conv := &Conversation{
		ID:       id,
		Messages: []Message{},
	}
	c.conversations[id] = conv
	return conv
}

// AddUserMessage adds a user message to the conversation
func (c *Conversation) AddUserMessage(content string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Messages = append(c.Messages, Message{
		Role:    "user",
		Content: content,
	})
}

// AddTherapistMessage adds a therapist message to the conversation
func (c *Conversation) AddTherapistMessage(content string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.Messages = append(c.Messages, Message{
		Role:    "therapist",
		Content: content,
	})
}

// GetFormattedHistory returns the conversation history formatted for the LLM
func (c *Conversation) GetFormattedHistory() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var history []string
	for _, msg := range c.Messages {
		if msg.Role == "user" {
			history = append(history, "User: "+msg.Content)
		} else {
			history = append(history, "Therapist: "+msg.Content)
		}
	}

	return history
}
