package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/ghophp/call-me-help/services"

	speech "cloud.google.com/go/speech/apiv1"
	"cloud.google.com/go/speech/apiv1/speechpb"
)

// TestTTSSTTLoop tests the Text-to-Speech and Speech-to-Text services in a loop
func TestTTSSTTLoop(serviceContainer *services.ServiceContainer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Println("Testing TTS -> STT service loop...")

		// Get input text from query parameters or use default
		inputText := r.URL.Query().Get("text")
		if inputText == "" {
			inputText = "Hello world, this is a test of speech services."
		}

		log.Printf("Input text: %q", inputText)

		// Create context with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Step 1: Convert text to speech using TTS service
		log.Printf("Step 1: Converting text to speech...")
		audioBytes, err := serviceContainer.TextToSpeech.SynthesizeSpeech(ctx, inputText)
		if err != nil {
			log.Printf("ERROR: Failed to synthesize speech: %v", err)
			http.Error(w, fmt.Sprintf("Failed to synthesize speech: %v", err), http.StatusInternalServerError)
			return
		}

		log.Printf("Successfully synthesized %d bytes of audio", len(audioBytes))

		// Step 2: Convert speech back to text using STT service
		log.Printf("Step 2: Converting speech back to text...")

		// Create recognition request
		client, err := speech.NewClient(ctx)
		if err != nil {
			log.Printf("ERROR: Failed to create Speech-to-Text client: %v", err)
			http.Error(w, fmt.Sprintf("Failed to create Speech-to-Text client: %v", err), http.StatusInternalServerError)
			return
		}
		defer client.Close()

		// Set up recognition request
		resp, err := client.Recognize(ctx, &speechpb.RecognizeRequest{
			Config: &speechpb.RecognitionConfig{
				Encoding:                   speechpb.RecognitionConfig_MULAW,
				SampleRateHertz:            8000, // Match TTS sample rate
				LanguageCode:               "en-US",
				EnableAutomaticPunctuation: true,
				UseEnhanced:                true,
				// Add speech contexts to improve recognition
				SpeechContexts: []*speechpb.SpeechContext{
					{
						Phrases: strings.Split(inputText, " "), // Use input words as hints
						Boost:   5.0,
					},
				},
			},
			Audio: &speechpb.RecognitionAudio{
				AudioSource: &speechpb.RecognitionAudio_Content{
					Content: audioBytes,
				},
			},
		})

		if err != nil {
			log.Printf("ERROR: Failed to recognize speech: %v", err)
			http.Error(w, fmt.Sprintf("Failed to recognize speech: %v", err), http.StatusInternalServerError)
			return
		}

		// Process the recognition results
		var transcriptions []string
		for _, result := range resp.Results {
			for _, alt := range result.Alternatives {
				transcriptions = append(transcriptions, alt.Transcript)
				log.Printf("Transcription: %q (confidence: %v)", alt.Transcript, alt.Confidence)
			}
		}

		// Calculate similarity between input and output text
		var similarity float64 = 0
		var bestMatch string
		if len(transcriptions) > 0 {
			// Simple similarity calculation - could be improved with proper NLP
			inputWords := strings.Fields(strings.ToLower(inputText))
			for _, transcript := range transcriptions {
				outputWords := strings.Fields(strings.ToLower(transcript))
				matches := 0
				for _, inputWord := range inputWords {
					for _, outputWord := range outputWords {
						if inputWord == outputWord {
							matches++
							break
						}
					}
				}

				// Calculate similarity as percentage of matched words
				currentSimilarity := float64(matches) / float64(len(inputWords)) * 100
				if currentSimilarity > similarity {
					similarity = currentSimilarity
					bestMatch = transcript
				}
			}
		}

		// Prepare response
		response := map[string]interface{}{
			"input_text":     inputText,
			"audio_size":     len(audioBytes),
			"transcriptions": transcriptions,
			"best_match":     bestMatch,
			"similarity":     similarity,
			"success":        len(transcriptions) > 0,
		}

		// Return the results as JSON
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}
}

// TestSpeechUI provides a simple HTML interface for testing speech services
func TestSpeechUI(w http.ResponseWriter, r *http.Request) {
	html := `<!DOCTYPE html>
<html>
<head>
    <title>Speech Services Test</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 0; padding: 20px; line-height: 1.6; }
        h1 { color: #333; }
        .container { max-width: 800px; margin: 0 auto; }
        .card { background: #f9f9f9; border-radius: 5px; padding: 20px; margin-bottom: 20px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        input[type=text], textarea { width: 100%; padding: 8px; margin: 8px 0; display: inline-block; border: 1px solid #ccc; border-radius: 4px; box-sizing: border-box; }
        button { background-color: #4CAF50; color: white; padding: 10px 15px; margin: 8px 0; border: none; border-radius: 4px; cursor: pointer; }
        button:hover { background-color: #45a049; }
        .results { background: #fff; border: 1px solid #ddd; padding: 15px; border-radius: 4px; white-space: pre-wrap; }
        .test-section { margin-bottom: 30px; }
        .file-input { margin: 10px 0; }
        select { padding: 8px; border-radius: 4px; border: 1px solid #ccc; }
    </style>
</head>
<body>
    <div class="container">
        <h1>Speech Services Test Console</h1>
        
        <div class="card test-section">
            <h2>Test 1: Text-to-Speech to Speech-to-Text Loop</h2>
            <p>This test converts text to speech and then back to text to verify both services are functioning.</p>
            
            <div>
                <label for="input-text">Enter text to convert:</label>
                <input type="text" id="input-text" value="Hello world, this is a test of speech services." style="width: 100%;">
                <button id="test-loop-btn">Run Test</button>
            </div>
            
            <h3>Results:</h3>
            <pre id="loop-results" class="results">Results will appear here...</pre>
        </div>
        
        <div class="card test-section">
            <h2>Test 2: Speech-to-Text with Generated Sample</h2>
            <p>This test generates a sample audio pattern and attempts to recognize it.</p>
            
            <div>
                <label for="sample-phrase">Test phrase:</label>
                <input type="text" id="sample-phrase" value="hello world test speech to text">
                <button id="test-sample-btn">Run Test</button>
            </div>
            
            <h3>Results:</h3>
            <pre id="sample-results" class="results">Results will appear here...</pre>
        </div>
        
        <div class="card test-section">
            <h2>Test 3: Speech-to-Text with File Upload</h2>
            <p>Upload an audio file to test speech recognition.</p>
            
            <form id="file-upload-form" enctype="multipart/form-data">
                <div class="file-input">
                    <label for="audio-file">Audio file:</label>
                    <input type="file" id="audio-file" name="audio" accept="audio/*">
                </div>
                
                <div>
                    <label for="encoding">Encoding:</label>
                    <select id="encoding" name="encoding">
                        <option value="LINEAR16">LINEAR16 (WAV)</option>
                        <option value="FLAC">FLAC</option>
                        <option value="MP3">MP3</option>
                        <option value="MULAW">MULAW</option>
                        <option value="OGG_OPUS">OGG_OPUS</option>
                    </select>
                    
                    <label for="sample-rate">Sample Rate (Hz):</label>
                    <input type="number" id="sample-rate" name="sample_rate" value="16000" style="width: 100px;">
                    
                    <label for="language-code">Language:</label>
                    <select id="language-code" name="language_code">
                        <option value="en-US">English (US)</option>
                        <option value="en-GB">English (UK)</option>
                        <option value="es-ES">Spanish</option>
                        <option value="fr-FR">French</option>
                        <option value="de-DE">German</option>
                    </select>
                </div>
                
                <button type="submit">Upload and Recognize</button>
            </form>
            
            <h3>Results:</h3>
            <pre id="file-results" class="results">Results will appear here...</pre>
        </div>
    </div>
    
    <script>
        // Test 1: TTS-STT Loop
        document.getElementById('test-loop-btn').addEventListener('click', function() {
            const text = document.getElementById('input-text').value;
            const resultsEl = document.getElementById('loop-results');
            
            resultsEl.textContent = 'Processing...';
            
            fetch('/test/tts-stt-loop?text=' + encodeURIComponent(text))
                .then(response => response.json())
                .then(data => {
                    let output = 'Input: "' + data.input_text + '"\n';
                    output += 'Audio Size: ' + data.audio_size + ' bytes\n\n';
                    
                    if (data.success) {
                        output += 'Transcriptions:\n';
                        data.transcriptions.forEach((t, i) => {
                            output += '  ' + (i+1) + '. "' + t + '"\n';
                        });
                        
                        output += '\nBest Match: "' + data.best_match + '"\n';
                        output += 'Similarity: ' + data.similarity.toFixed(2) + '%';
                    } else {
                        output += 'No transcriptions found.';
                    }
                    
                    resultsEl.textContent = output;
                })
                .catch(error => {
                    resultsEl.textContent = 'Error: ' + error.message;
                });
        });
        
        // Test 2: Sample STT
        document.getElementById('test-sample-btn').addEventListener('click', function() {
            const phrase = document.getElementById('sample-phrase').value;
            const resultsEl = document.getElementById('sample-results');
            
            resultsEl.textContent = 'Processing...';
            
            fetch('/test/stt-sample?phrase=' + encodeURIComponent(phrase))
                .then(response => response.text())
                .then(data => {
                    resultsEl.textContent = data;
                })
                .catch(error => {
                    resultsEl.textContent = 'Error: ' + error.message;
                });
        });
        
        // Test 3: File Upload
        document.getElementById('file-upload-form').addEventListener('submit', function(e) {
            e.preventDefault();
            
            const resultsEl = document.getElementById('file-results');
            resultsEl.textContent = 'Uploading and processing...';
            
            const formData = new FormData(this);
            
            fetch('/test/stt-file', {
                method: 'POST',
                body: formData
            })
                .then(response => response.json())
                .then(data => {
                    if (data.success) {
                        let output = 'Transcriptions:\n';
                        data.transcriptions.forEach((t, i) => {
                            output += '  ' + (i+1) + '. "' + t + '"\n';
                        });
                        resultsEl.textContent = output;
                    } else {
                        resultsEl.textContent = 'No transcriptions found: ' + data.message;
                    }
                })
                .catch(error => {
                    resultsEl.textContent = 'Error: ' + error.message;
                });
        });
    </script>
</body>
</html>`

	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, html)
}
