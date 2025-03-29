package services

import (
	"bytes"
	"context"
	"io"
	"log"
	"sync"
)

// ChannelData holds the channels for a specific call
type ChannelData struct {
	CallSID              string
	AudioInputChan       chan []byte
	TranscriptionChan    chan string
	ResponseTextChan     chan string
	ResponseAudioChan    chan []byte
	audioBuffer          *bytes.Buffer
	isProcessingAudio    bool
	processingAudioMutex sync.Mutex
}

// ChannelManager manages communication channels for active calls
type ChannelManager struct {
	channels map[string]*ChannelData
	mu       sync.Mutex
}

// NewChannelManager creates a new channel manager
func NewChannelManager() *ChannelManager {
	return &ChannelManager{
		channels: make(map[string]*ChannelData),
	}
}

// CreateChannels creates channels for a new call
func (cm *ChannelManager) CreateChannels(callSID string) *ChannelData {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	channels := &ChannelData{
		CallSID:           callSID,
		AudioInputChan:    make(chan []byte, 100),
		TranscriptionChan: make(chan string, 10),
		ResponseTextChan:  make(chan string, 10),
		ResponseAudioChan: make(chan []byte, 10),
		audioBuffer:       &bytes.Buffer{},
	}

	cm.channels[callSID] = channels
	return channels
}

// GetChannels retrieves channels for a call
func (cm *ChannelManager) GetChannels(callSID string) (*ChannelData, bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	channels, ok := cm.channels[callSID]
	return channels, ok
}

// RemoveChannels removes channels for a call
func (cm *ChannelManager) RemoveChannels(callSID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	delete(cm.channels, callSID)
}

// StartAudioProcessing starts processing audio through speech-to-text
func (cm *ChannelManager) StartAudioProcessing(ctx context.Context, callSID string, stt *SpeechToTextService) {
	channels, ok := cm.GetChannels(callSID)
	if !ok {
		log.Printf("No channels found for call %s", callSID)
		return
	}

	channels.processingAudioMutex.Lock()
	if channels.isProcessingAudio {
		channels.processingAudioMutex.Unlock()
		return
	}
	channels.isProcessingAudio = true
	channels.processingAudioMutex.Unlock()

	// Create a pipe for streaming the audio data
	pipeReader, pipeWriter := io.Pipe()

	// Start streaming recognition
	transcriptionChan, err := stt.StreamingRecognize(ctx, pipeReader)
	if err != nil {
		log.Printf("Error starting streaming recognition: %v", err)
		return
	}

	// Forward audio data to the pipe
	go func() {
		defer pipeWriter.Close()
		for audioData := range channels.AudioInputChan {
			if _, err := pipeWriter.Write(audioData); err != nil {
				log.Printf("Error writing to pipe: %v", err)
				break
			}
		}
	}()

	// Forward transcriptions to the transcription channel
	go func() {
		for transcription := range transcriptionChan {
			channels.TranscriptionChan <- transcription
		}
	}()
}

// ProcessVoiceToText processes voice data for a call
func (cd *ChannelData) AppendAudioData(data []byte) {
	cd.processingAudioMutex.Lock()
	defer cd.processingAudioMutex.Unlock()

	// Add data to the audio buffer
	cd.audioBuffer.Write(data)

	// Send data to the audio input channel
	cd.AudioInputChan <- data
}

// Reset clears the audio buffer
func (cd *ChannelData) ResetAudioBuffer() {
	cd.processingAudioMutex.Lock()
	defer cd.processingAudioMutex.Unlock()

	cd.audioBuffer.Reset()
}
