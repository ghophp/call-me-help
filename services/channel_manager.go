package services

import (
	"bytes"
	"context"
	"io"
	"log"
	"sync"
	"time"
)

// ChannelData holds the channels for a specific call
type ChannelData struct {
	CallSID              string
	CreatedAt            time.Time
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
	log.Printf("Creating new ChannelManager")
	return &ChannelManager{
		channels: make(map[string]*ChannelData),
	}
}

// CreateChannels creates channels for a new call
func (cm *ChannelManager) CreateChannels(callSID string) *ChannelData {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	log.Printf("Creating channels for call %s", callSID)
	channels := &ChannelData{
		CallSID:           callSID,
		CreatedAt:         time.Now(),
		AudioInputChan:    make(chan []byte, 100),
		TranscriptionChan: make(chan string, 10),
		ResponseTextChan:  make(chan string, 10),
		ResponseAudioChan: make(chan []byte, 10),
		audioBuffer:       &bytes.Buffer{},
	}

	cm.channels[callSID] = channels
	log.Printf("Created channels for call %s", callSID)
	return channels
}

// GetChannels retrieves channels for a call
func (cm *ChannelManager) GetChannels(callSID string) (*ChannelData, bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	channels, ok := cm.channels[callSID]
	if !ok {
		log.Printf("Channels not found for call %s", callSID)
	} else {
		log.Printf("Retrieved channels for call %s", callSID)
	}
	return channels, ok
}

// RemoveChannels removes channels for a call
func (cm *ChannelManager) RemoveChannels(callSID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	log.Printf("Removing channels for call %s", callSID)
	delete(cm.channels, callSID)
	log.Printf("Removed channels for call %s", callSID)
}

// GetMostRecentCallSID returns the SID of the most recently created call
func (cm *ChannelManager) GetMostRecentCallSID() string {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	var mostRecentSID string
	var mostRecentTime time.Time

	for sid, channel := range cm.channels {
		if mostRecentSID == "" || channel.CreatedAt.After(mostRecentTime) {
			mostRecentSID = sid
			mostRecentTime = channel.CreatedAt
		}
	}

	if mostRecentSID != "" {
		log.Printf("Found most recent call SID: %s", mostRecentSID)
	} else {
		log.Printf("No active calls found")
	}

	return mostRecentSID
}

// StartAudioProcessing starts processing audio through speech-to-text
func (cm *ChannelManager) StartAudioProcessing(ctx context.Context, callSID string, stt *SpeechToTextService) {
	log.Printf("Starting audio processing for call %s", callSID)
	channels, ok := cm.GetChannels(callSID)
	if !ok {
		log.Printf("No channels found for call %s, cannot start audio processing", callSID)
		return
	}

	channels.processingAudioMutex.Lock()
	if channels.isProcessingAudio {
		log.Printf("Audio processing already in progress for call %s", callSID)
		channels.processingAudioMutex.Unlock()
		return
	}
	channels.isProcessingAudio = true
	channels.processingAudioMutex.Unlock()
	log.Printf("Audio processing flag set for call %s", callSID)

	// Create a pipe for streaming the audio data
	log.Printf("Creating pipe for audio streaming for call %s", callSID)
	pipeReader, pipeWriter := io.Pipe()

	// Start streaming recognition
	log.Printf("Initiating Speech-to-Text streaming for call %s", callSID)
	transcriptionChan, err := stt.StreamingRecognize(ctx, pipeReader)
	if err != nil {
		log.Printf("Error starting streaming recognition for call %s: %v", callSID, err)
		return
	}
	log.Printf("Speech-to-Text streaming started for call %s", callSID)

	// Forward audio data to the pipe
	go func() {
		log.Printf("Starting audio forwarding goroutine for call %s", callSID)
		defer pipeWriter.Close()

		for audioData := range channels.AudioInputChan {
			log.Printf("Received %d bytes of audio data for call %s", len(audioData), callSID)
			if _, err := pipeWriter.Write(audioData); err != nil {
				log.Printf("Error writing to pipe for call %s: %v", callSID, err)
				break
			}
			log.Printf("Wrote %d bytes to STT pipe for call %s", len(audioData), callSID)
		}

		log.Printf("Audio forwarding goroutine ended for call %s", callSID)
	}()

	// Forward transcriptions to the transcription channel
	go func() {
		log.Printf("Starting transcription forwarding goroutine for call %s", callSID)

		for transcription := range transcriptionChan {
			log.Printf("Received transcription from Google STT for call %s: %s", callSID, transcription)
			channels.TranscriptionChan <- transcription
			log.Printf("Forwarded transcription to channel for call %s", callSID)
		}

		log.Printf("Transcription forwarding goroutine ended for call %s", callSID)
	}()

	log.Printf("Audio processing successfully started for call %s", callSID)
}

// AppendAudioData adds audio data to the buffer and input channel
func (cd *ChannelData) AppendAudioData(data []byte) {
	cd.processingAudioMutex.Lock()
	defer cd.processingAudioMutex.Unlock()

	// Add data to the audio buffer
	log.Printf("Appending %d bytes of audio data for call %s", len(data), cd.CallSID)
	cd.audioBuffer.Write(data)

	// Send data to the audio input channel
	select {
	case cd.AudioInputChan <- data:
		log.Printf("Sent %d bytes to audio input channel for call %s", len(data), cd.CallSID)
	default:
		log.Printf("Warning: Audio input channel full for call %s, dropping data", cd.CallSID)
	}
}

// ResetAudioBuffer clears the audio buffer
func (cd *ChannelData) ResetAudioBuffer() {
	cd.processingAudioMutex.Lock()
	defer cd.processingAudioMutex.Unlock()

	log.Printf("Resetting audio buffer for call %s", cd.CallSID)
	cd.audioBuffer.Reset()
	log.Printf("Audio buffer reset for call %s", cd.CallSID)
}
