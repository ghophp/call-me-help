package services

import (
	"context"
	"errors"
	"sync"
	"time"

	"cloud.google.com/go/speech/apiv1/speechpb"
	"github.com/ghophp/call-me-help/logger"
)

// ChannelData holds the channels for a specific call
type ChannelData struct {
	CallSID              string
	CreatedAt            time.Time
	AudioInputChan       chan []byte
	TranscriptionChan    chan string
	ResponseTextChan     chan string
	ResponseAudioChan    chan []byte
	isProcessingAudio    bool
	processingAudioMutex sync.Mutex
}

// ChannelManager manages communication channels for active calls
type ChannelManager struct {
	channels map[string]*ChannelData
	mu       sync.Mutex
	log      *logger.Logger
}

// NewChannelManager creates a new channel manager
func NewChannelManager() *ChannelManager {
	log := logger.Component("ChannelManager")
	log.Info("Creating new ChannelManager")
	return &ChannelManager{
		channels: make(map[string]*ChannelData),
		log:      log,
	}
}

// CreateChannels creates channels for a new call
func (cm *ChannelManager) CreateChannels(callSID string) *ChannelData {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.log.Info("Creating channels for call %s", callSID)
	channels := &ChannelData{
		CallSID:           callSID,
		CreatedAt:         time.Now(),
		AudioInputChan:    make(chan []byte, 1024),
		TranscriptionChan: make(chan string, 1024),
		ResponseTextChan:  make(chan string, 1024),
		ResponseAudioChan: make(chan []byte, 1024),
	}

	cm.channels[callSID] = channels
	cm.log.Info("Created channels for call %s", callSID)
	return channels
}

// GetChannels retrieves channels for a call
func (cm *ChannelManager) GetChannels(callSID string) (*ChannelData, bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	channels, ok := cm.channels[callSID]
	if !ok {
		cm.log.Warn("Channels not found for call %s", callSID)
	} else {
		cm.log.Debug("Retrieved channels for call %s", callSID)
	}
	return channels, ok
}

// RemoveChannels removes channels for a call
func (cm *ChannelManager) RemoveChannels(callSID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.log.Info("Removing channels for call %s", callSID)
	delete(cm.channels, callSID)
	cm.log.Info("Removed channels for call %s", callSID)
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
		cm.log.Info("Found most recent call SID: %s", mostRecentSID)
	} else {
		cm.log.Warn("No active calls found")
	}

	return mostRecentSID
}

// StartAudioProcessing starts processing audio through speech-to-text
func (cm *ChannelManager) StartAudioProcessing(ctx context.Context, callSID string, stt *SpeechToTextService) (speechpb.Speech_StreamingRecognizeClient, error) {
	cm.log.Info("Starting audio processing for call %s", callSID)
	channels, ok := cm.GetChannels(callSID)
	if !ok {
		cm.log.Error("No channels found for call %s, cannot start audio processing", callSID)
		return nil, errors.New("no channels found for call")
	}

	// Set processing flag to avoid multiple processors for same call
	channels.processingAudioMutex.Lock()
	if channels.isProcessingAudio {
		cm.log.Warn("Audio processing already in progress for call %s", callSID)
		channels.processingAudioMutex.Unlock()
		return nil, errors.New("audio processing already in progress")
	}
	channels.isProcessingAudio = true
	channels.processingAudioMutex.Unlock()
	cm.log.Debug("Audio processing flag set for call %s", callSID)

	// Create a pipe for streaming the audio data
	cm.log.Debug("Creating pipe for audio streaming for call %s", callSID)

	// Start streaming recognition
	cm.log.Info("Initiating Speech-to-Text streaming for call %s", callSID)
	transcriptionChan, stream, err := stt.StreamingRecognize(ctx)
	if err != nil {
		cm.log.Error("Error starting streaming recognition for call %s: %v", callSID, err)
		return nil, err
	}
	cm.log.Info("Speech-to-Text streaming started for call %s", callSID)

	// Forward transcriptions to the transcription channel
	go func() {
		cm.log.Debug("Starting transcription forwarding goroutine for call %s", callSID)
		defer cm.log.Debug("Transcription forwarding goroutine ended for call %s", callSID)

		transcriptionCount := 0
		for transcription := range transcriptionChan {
			transcriptionCount++
			cm.log.Debug("Received transcription #%d from Google STT for call %s: %s",
				transcriptionCount, callSID, transcription)

			select {
			case channels.TranscriptionChan <- transcription:
				cm.log.Debug("Forwarded transcription #%d to channel for call %s",
					transcriptionCount, callSID)
			default:
				cm.log.Warn("TranscriptionChan full for call %s, dropping transcription: %s",
					callSID, transcription)
			}
		}

		cm.log.Info("Transcription channel closed after %d transcriptions for call %s",
			transcriptionCount, callSID)
	}()

	cm.log.Info("Audio processing successfully started for call %s", callSID)
	return stream, nil
}

// AppendAudioData adds audio data to the buffer and input channel
func (cd *ChannelData) AppendAudioData(data []byte) {
	cd.processingAudioMutex.Lock()
	defer cd.processingAudioMutex.Unlock()

	// Skip empty data
	if len(data) == 0 {
		logger.Debug("Skipping empty audio data for call %s", cd.CallSID)
		return
	}

	// Add data to the audio buffer
	logger.Debug("Appending %d bytes of audio data for call %s", len(data), cd.CallSID)

	// Write to buffer
	cd.AudioInputChan <- data
}
