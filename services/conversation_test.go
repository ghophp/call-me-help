package services

import (
	"testing"
)

func TestConversationService(t *testing.T) {
	// Create a new conversation service
	service := NewConversationService()
	if service == nil {
		t.Fatal("Failed to create conversation service")
	}

	// Test GetOrCreateConversation
	callID := "test-call-123"
	conv := service.GetOrCreateConversation(callID)
	if conv == nil {
		t.Fatal("Failed to create conversation")
	}
	if conv.ID != callID {
		t.Errorf("Expected conversation ID %s, got %s", callID, conv.ID)
	}
	if len(conv.Messages) != 0 {
		t.Errorf("Expected 0 messages, got %d", len(conv.Messages))
	}

	// Test getting the same conversation again
	conv2 := service.GetOrCreateConversation(callID)
	if conv != conv2 {
		t.Error("GetOrCreateConversation should return the same conversation instance for the same ID")
	}

	// Test adding messages
	testUserMsg := "Hello, I'm feeling sad today."
	conv.AddUserMessage(testUserMsg)
	if len(conv.Messages) != 1 {
		t.Errorf("Expected 1 message, got %d", len(conv.Messages))
	}
	if conv.Messages[0].Role != "user" || conv.Messages[0].Content != testUserMsg {
		t.Errorf("Message not added correctly: %+v", conv.Messages[0])
	}

	testTherapistMsg := "I'm sorry to hear that. Can you tell me more about what's bothering you?"
	conv.AddTherapistMessage(testTherapistMsg)
	if len(conv.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(conv.Messages))
	}
	if conv.Messages[1].Role != "therapist" || conv.Messages[1].Content != testTherapistMsg {
		t.Errorf("Message not added correctly: %+v", conv.Messages[1])
	}

	// Test GetFormattedHistory
	history := conv.GetFormattedHistory()
	if len(history) != 2 {
		t.Errorf("Expected 2 history entries, got %d", len(history))
	}
	if history[0] != "User: "+testUserMsg {
		t.Errorf("Expected 'User: %s', got '%s'", testUserMsg, history[0])
	}
	if history[1] != "Therapist: "+testTherapistMsg {
		t.Errorf("Expected 'Therapist: %s', got '%s'", testTherapistMsg, history[1])
	}
}
