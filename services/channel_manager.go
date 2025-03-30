package services

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"cloud.google.com/go/speech/apiv1/speechpb"
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
		AudioInputChan:    make(chan []byte, 1024),
		TranscriptionChan: make(chan string, 1024),
		ResponseTextChan:  make(chan string, 1024),
		ResponseAudioChan: make(chan []byte, 1024),
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
func (cm *ChannelManager) StartAudioProcessing(ctx context.Context, callSID string, stt *SpeechToTextService) (speechpb.Speech_StreamingRecognizeClient, error) {
	log.Printf("Starting audio processing for call %s", callSID)
	channels, ok := cm.GetChannels(callSID)
	if !ok {
		log.Printf("No channels found for call %s, cannot start audio processing", callSID)
		return nil, errors.New("no channels found for call")
	}

	// Set processing flag to avoid multiple processors for same call
	channels.processingAudioMutex.Lock()
	if channels.isProcessingAudio {
		log.Printf("Audio processing already in progress for call %s", callSID)
		channels.processingAudioMutex.Unlock()
		return nil, errors.New("audio processing already in progress")
	}
	channels.isProcessingAudio = true
	channels.processingAudioMutex.Unlock()
	log.Printf("Audio processing flag set for call %s", callSID)

	// Create a pipe for streaming the audio data
	log.Printf("Creating pipe for audio streaming for call %s", callSID)

	// Start streaming recognition
	log.Printf("Initiating Speech-to-Text streaming for call %s", callSID)
	transcriptionChan, stream, err := stt.StreamingRecognize(ctx)
	if err != nil {
		log.Printf("Error starting streaming recognition for call %s: %v", callSID, err)
		return nil, err
	}
	log.Printf("Speech-to-Text streaming started for call %s", callSID)

	// Forward transcriptions to the transcription channel
	go func() {
		log.Printf("Starting transcription forwarding goroutine for call %s", callSID)
		defer log.Printf("Transcription forwarding goroutine ended for call %s", callSID)

		transcriptionCount := 0
		for transcription := range transcriptionChan {
			transcriptionCount++
			log.Printf("Received transcription #%d from Google STT for call %s: %s",
				transcriptionCount, callSID, transcription)

			select {
			case channels.TranscriptionChan <- transcription:
				log.Printf("Forwarded transcription #%d to channel for call %s",
					transcriptionCount, callSID)
			default:
				log.Printf("WARNING: TranscriptionChan full for call %s, dropping transcription: %s",
					callSID, transcription)
			}
		}

		log.Printf("Transcription channel closed after %d transcriptions for call %s",
			transcriptionCount, callSID)
	}()

	log.Printf("Audio processing successfully started for call %s", callSID)
	return stream, nil
}

// AppendAudioData adds audio data to the buffer and input channel
func (cd *ChannelData) AppendAudioData(data []byte) {
	cd.processingAudioMutex.Lock()
	defer cd.processingAudioMutex.Unlock()

	// Skip empty data
	if len(data) == 0 {
		log.Printf("Skipping empty audio data for call %s", cd.CallSID)
		return
	}

	// Add data to the audio buffer
	log.Printf("Appending %d bytes of audio data for call %s", len(data), cd.CallSID)

	// Write to buffer
	cd.AudioInputChan <- data
}
