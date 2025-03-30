package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"sync"
)

// Level defines the logging level
type Level int

const (
	// DEBUG level for verbose debugging information
	DEBUG Level = iota
	// INFO level for general information
	INFO
	// WARN level for warning messages
	WARN
	// ERROR level for error messages
	ERROR
)

var levelNames = map[Level]string{
	DEBUG: "DEBUG",
	INFO:  "INFO",
	WARN:  "WARN",
	ERROR: "ERROR",
}

// Logger handles logging with different levels
type Logger struct {
	level     Level
	mu        sync.Mutex
	logger    *log.Logger
	component string
}

var (
	defaultLogger *Logger
	once          sync.Once
)

// Initialize initializes the default logger with the specified level
func Initialize(level Level) {
	once.Do(func() {
		defaultLogger = NewLogger(os.Stdout, level, "")
		log.SetOutput(io.Discard) // Redirect standard logger to discard
	})
}

// SetLevel sets the logging level for the default logger
func SetLevel(level Level) {
	if defaultLogger != nil {
		defaultLogger.SetLevel(level)
	}
}

// NewLogger creates a new logger with the specified writer and level
func NewLogger(out io.Writer, level Level, component string) *Logger {
	return &Logger{
		level:     level,
		logger:    log.New(out, "", log.LstdFlags|log.Lshortfile),
		component: component,
	}
}

// SetLevel sets the logging level for this logger
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// log logs a message at the specified level
func (l *Logger) log(level Level, format string, v ...interface{}) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	prefix := fmt.Sprintf("[%s]", levelNames[level])
	if l.component != "" {
		prefix = fmt.Sprintf("%s[%s]", prefix, l.component)
	}

	msg := fmt.Sprintf(format, v...)
	l.logger.Output(3, fmt.Sprintf("%s %s", prefix, msg))
}

// Debug logs a debug message
func (l *Logger) Debug(format string, v ...interface{}) {
	l.log(DEBUG, format, v...)
}

// Info logs an info message
func (l *Logger) Info(format string, v ...interface{}) {
	l.log(INFO, format, v...)
}

// Warn logs a warning message
func (l *Logger) Warn(format string, v ...interface{}) {
	l.log(WARN, format, v...)
}

// Error logs an error message
func (l *Logger) Error(format string, v ...interface{}) {
	l.log(ERROR, format, v...)
}

// Component returns a new logger with the specified component name
func (l *Logger) Component(name string) *Logger {
	return &Logger{
		level:     l.level,
		logger:    l.logger,
		component: name,
	}
}

// GetDefaultLogger returns the default logger
func GetDefaultLogger() *Logger {
	if defaultLogger == nil {
		// Initialize with INFO level if not initialized yet
		Initialize(INFO)
	}
	return defaultLogger
}

// Debug logs a debug message using the default logger
func Debug(format string, v ...interface{}) {
	GetDefaultLogger().Debug(format, v...)
}

// Info logs an info message using the default logger
func Info(format string, v ...interface{}) {
	GetDefaultLogger().Info(format, v...)
}

// Warn logs a warning message using the default logger
func Warn(format string, v ...interface{}) {
	GetDefaultLogger().Warn(format, v...)
}

// Error logs an error message using the default logger
func Error(format string, v ...interface{}) {
	GetDefaultLogger().Error(format, v...)
}

// Component returns a new logger with the specified component name
func Component(name string) *Logger {
	return GetDefaultLogger().Component(name)
}
