package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestLogLevels(t *testing.T) {
	// Create a buffer to capture log output
	buf := new(bytes.Buffer)

	// Create a logger with DEBUG level
	logger := NewLogger(buf, DEBUG, "TestLogger")

	// Test that all levels are logged
	logger.Debug("Debug message")
	logger.Info("Info message")
	logger.Warn("Warn message")
	logger.Error("Error message")

	output := buf.String()

	// Check each level is present
	if !strings.Contains(output, "[DEBUG][TestLogger] Debug message") {
		t.Error("DEBUG level message not logged correctly")
	}
	if !strings.Contains(output, "[INFO][TestLogger] Info message") {
		t.Error("INFO level message not logged correctly")
	}
	if !strings.Contains(output, "[WARN][TestLogger] Warn message") {
		t.Error("WARN level message not logged correctly")
	}
	if !strings.Contains(output, "[ERROR][TestLogger] Error message") {
		t.Error("ERROR level message not logged correctly")
	}

	// Create a new buffer and logger with INFO level
	buf = new(bytes.Buffer)
	logger = NewLogger(buf, INFO, "TestLogger")

	// Test level filtering
	logger.Debug("Debug message") // Should be filtered
	logger.Info("Info message")   // Should be logged
	logger.Warn("Warn message")   // Should be logged
	logger.Error("Error message") // Should be logged

	output = buf.String()

	// DEBUG should be filtered out
	if strings.Contains(output, "[DEBUG][TestLogger] Debug message") {
		t.Error("DEBUG level message was logged when it should have been filtered")
	}

	// Other levels should be present
	if !strings.Contains(output, "[INFO][TestLogger] Info message") {
		t.Error("INFO level message not logged correctly")
	}
	if !strings.Contains(output, "[WARN][TestLogger] Warn message") {
		t.Error("WARN level message not logged correctly")
	}
	if !strings.Contains(output, "[ERROR][TestLogger] Error message") {
		t.Error("ERROR level message not logged correctly")
	}
}

func TestLoggerComponent(t *testing.T) {
	// Create a buffer to capture log output
	buf := new(bytes.Buffer)

	// Create a base logger with DEBUG level
	baseLogger := NewLogger(buf, DEBUG, "Base")

	// Create a component logger from the base logger
	componentLogger := baseLogger.Component("Component")

	// Test that component name is included in logs
	componentLogger.Info("Component log")

	output := buf.String()

	if !strings.Contains(output, "[INFO][Component] Component log") {
		t.Error("Component name not included in log output")
	}
}

func TestDefaultLogger(t *testing.T) {
	// Initialize logger with INFO level
	Initialize(INFO)

	// Get default logger
	logger := GetDefaultLogger()

	// Check level
	if logger.level != INFO {
		t.Errorf("Expected default logger level to be INFO, got %v", logger.level)
	}

	// Change level
	SetLevel(ERROR)

	// Check new level
	if logger.level != ERROR {
		t.Errorf("Expected default logger level to be ERROR, got %v", logger.level)
	}
}
