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

	// Set processing flag to avoid multiple processors for same call
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

	// Buffer for aggregating small audio chunks
	var audioBuffer bytes.Buffer
	const minChunkSize = 4000 // Process in larger chunks for better STT performance

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
		defer log.Printf("Audio forwarding goroutine ended for call %s", callSID)

		// Process audio from the channel
		var totalBytesProcessed int64
		lastFlushTime := time.Now()

		flushBuffer := func() {
			if audioBuffer.Len() == 0 {
				return
			}

			bufBytes := audioBuffer.Bytes()
			log.Printf("Flushing %d bytes to STT pipe for call %s", len(bufBytes), callSID)

			if _, err := pipeWriter.Write(bufBytes); err != nil {
				log.Printf("Error writing to pipe for call %s: %v", callSID, err)
				return
			}

			totalBytesProcessed += int64(len(bufBytes))
			log.Printf("Wrote %d bytes to STT pipe (%d total) for call %s",
				len(bufBytes), totalBytesProcessed, callSID)

			audioBuffer.Reset()
			lastFlushTime = time.Now()
		}

		// Ticker to ensure we flush data periodically even if we're not getting enough
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				log.Printf("Context canceled, stopping audio forwarding for call %s", callSID)
				flushBuffer() // Final flush
				return

			case <-ticker.C:
				// Periodically flush data if it's been too long
				if audioBuffer.Len() > 0 && time.Since(lastFlushTime) > 500*time.Millisecond {
					log.Printf("Flushing audio buffer due to timeout, %d bytes", audioBuffer.Len())
					flushBuffer()
				}

			case audioData, ok := <-channels.AudioInputChan:
				if !ok {
					log.Printf("Audio channel closed for call %s", callSID)
					flushBuffer() // Final flush
					return
				}

				if len(audioData) == 0 {
					continue // Skip empty data
				}

				// Append to buffer
				audioBuffer.Write(audioData)

				// Flush if buffer is large enough
				if audioBuffer.Len() >= minChunkSize {
					flushBuffer()
				}
			}
		}
	}()

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
	if _, err := cd.audioBuffer.Write(data); err != nil {
		log.Printf("Error writing to audio buffer: %v", err)
	}

	// Send data to the audio input channel with retry
	for attempts := 0; attempts < 3; attempts++ {
		select {
		case cd.AudioInputChan <- data:
			log.Printf("Sent %d bytes to audio input channel for call %s", len(data), cd.CallSID)
			return
		default:
			if attempts < 2 {
				// Try to read from the channel to make space
				log.Printf("Audio input channel full for call %s, clearing space (attempt %d)", cd.CallSID, attempts+1)
				select {
				case <-cd.AudioInputChan:
					log.Printf("Removed oldest audio chunk to make space")
				default:
					// If we can't read immediately, just continue to next attempt
					time.Sleep(10 * time.Millisecond)
				}
			} else {
				log.Printf("WARNING: Audio input channel full for call %s after retries, dropping data", cd.CallSID)
			}
		}
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
