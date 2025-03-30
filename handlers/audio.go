package handlers

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ghophp/call-me-help/config"
	"github.com/ghophp/call-me-help/logger"
)

// AudioFile represents metadata about a saved audio file
type AudioFile struct {
	Filename    string    `json:"filename"`
	CallSID     string    `json:"callSid"`
	Timestamp   time.Time `json:"timestamp"`
	Text        string    `json:"text"`
	SizeBytes   int64     `json:"sizeBytes"`
	DownloadURL string    `json:"downloadUrl"`
}

// ListAudioFiles handles the GET /audio endpoint to list all saved audio files
func ListAudioFiles() http.HandlerFunc {
	log := logger.Component("AudioHandler")
	cfg := config.Load()

	return func(w http.ResponseWriter, r *http.Request) {
		log.Info("Listing audio files")

		// Get directory to search
		audioDir := cfg.AudioOutputDirectory

		// Check if directory exists
		if _, err := os.Stat(audioDir); os.IsNotExist(err) {
			log.Info("Audio directory %s does not exist yet", audioDir)
			// Return empty array, not an error
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("[]"))
			return
		}

		// List files in the directory
		var files []AudioFile
		err := filepath.Walk(audioDir, func(path string, info fs.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Skip directories
			if info.IsDir() {
				return nil
			}

			// Skip files that don't have .raw extension
			if !strings.HasSuffix(info.Name(), ".raw") {
				return nil
			}

			// Parse filename to extract metadata
			// Format is: {callSID}_{timestamp}_{text}.raw
			filename := info.Name()
			parts := strings.SplitN(strings.TrimSuffix(filename, ".raw"), "_", 3)

			if len(parts) < 3 {
				log.Warn("Skipping file with invalid format: %s", filename)
				return nil
			}

			callSID := parts[0]

			// Parse timestamp (format: 20060102-150405.000)
			timestamp, err := time.Parse("20060102-150405.000", parts[1])
			if err != nil {
				log.Warn("Failed to parse timestamp for file %s: %v", filename, err)
				// Use file modification time as fallback
				timestamp = info.ModTime()
			}

			// Get the text part
			text := parts[2]

			// Create download URL
			host := r.Host
			scheme := "http"
			if r.TLS != nil {
				scheme = "https"
			}
			downloadURL := fmt.Sprintf("%s://%s/audio/download/%s", scheme, host, filename)

			// Create file info structure
			fileInfo := AudioFile{
				Filename:    filename,
				CallSID:     callSID,
				Timestamp:   timestamp,
				Text:        text,
				SizeBytes:   info.Size(),
				DownloadURL: downloadURL,
			}

			files = append(files, fileInfo)
			return nil
		})

		if err != nil {
			log.Error("Error listing audio files: %v", err)
			http.Error(w, "Failed to list audio files", http.StatusInternalServerError)
			return
		}

		// Sort files by timestamp (newest first)
		sort.Slice(files, func(i, j int) bool {
			return files[i].Timestamp.After(files[j].Timestamp)
		})

		// Return the list as JSON
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(files); err != nil {
			log.Error("Error encoding response: %v", err)
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
			return
		}

		log.Info("Successfully returned list of %d audio files", len(files))
	}
}

// DownloadAudioFile handles the GET /audio/download/{filename} endpoint to download a specific audio file
func DownloadAudioFile() http.HandlerFunc {
	log := logger.Component("AudioHandler")
	cfg := config.Load()

	return func(w http.ResponseWriter, r *http.Request) {
		// Extract filename from URL path
		urlPath := r.URL.Path
		filename := filepath.Base(urlPath)

		log.Info("Request to download audio file: %s", filename)

		// Validate filename to prevent directory traversal
		if strings.Contains(filename, "..") || strings.Contains(filename, "/") {
			log.Warn("Invalid filename requested: %s", filename)
			http.Error(w, "Invalid filename", http.StatusBadRequest)
			return
		}

		// Construct file path
		filePath := filepath.Join(cfg.AudioOutputDirectory, filename)

		// Check if file exists
		fileInfo, err := os.Stat(filePath)
		if os.IsNotExist(err) {
			log.Warn("Requested file not found: %s", filePath)
			http.Error(w, "File not found", http.StatusNotFound)
			return
		} else if err != nil {
			log.Error("Error checking file: %v", err)
			http.Error(w, "Error accessing file", http.StatusInternalServerError)
			return
		}

		// Open and serve the file
		file, err := os.Open(filePath)
		if err != nil {
			log.Error("Error opening file: %v", err)
			http.Error(w, "Error opening file", http.StatusInternalServerError)
			return
		}
		defer file.Close()

		// Set appropriate headers
		w.Header().Set("Content-Type", "audio/basic") // MIME type for μ-law audio
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
		w.Header().Set("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

		// Stream the file to the response
		http.ServeContent(w, r, filename, fileInfo.ModTime(), file)

		log.Info("Successfully served audio file: %s (%d bytes)", filename, fileInfo.Size())
	}
}
