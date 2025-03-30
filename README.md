# Call Me Help

A therapeutic voice assistant for mental health support, powered by Twilio, Google Cloud, and Gemini AI.

## Overview

Call Me Help is a telephony-based system that provides mental health support through an AI-driven conversational interface. Users can call a designated phone number to speak with an AI therapist that listens, responds empathetically, and provides guidance.

### Key Features

- Receive incoming phone calls via Twilio
- Stream audio bidirectionally via WebSockets
- Convert speech to text using Google Cloud Speech-to-Text
- Generate therapeutic responses using Gemini AI
- Convert text back to speech using Google Cloud Text-to-Speech
- Maintain conversation context for personalized interactions

## Architecture

The application follows a modular architecture:

- **Twilio Integration**: Handles incoming calls and media streams
- **WebSocket Server**: Manages bidirectional audio streaming
- **Speech Services**: Converts between audio and text
- **AI Service**: Generates appropriate therapeutic responses
- **Conversation Management**: Maintains context throughout the session

## Prerequisites

- Go 1.23 or higher
- Twilio account with a phone number
- Google Cloud account with:
  - Speech-to-Text API enabled
  - Text-to-Speech API enabled
  - Service account with appropriate permissions
- Gemini API key (can be obtained from Google AI Studio)

## Google Cloud Setup

1. Create a new project in Google Cloud Platform
2. Enable the following APIs:
   - Speech-to-Text API
   - Text-to-Speech API
3. Create a service account with the following permissions:
   - Speech-to-Text User (`roles/speech.client`)
   - Text-to-Speech User (`roles/texttospeech.user`) 
4. Download the service account key as a JSON file

## Gemini API Setup

1. Go to [Google AI Studio](https://makersuite.google.com/app/apikey)
2. Create a new API key 
3. Add this API key to your environment variables as `GEMINI_API_KEY`

## Setup

1. Clone the repository:
   ```bash
   git clone https://github.com/ghophp/call-me-help.git
   cd call-me-help
   ```

2. Install dependencies:
   ```bash
   go mod tidy
   ```

3. Set up environment variables in a `.env` file:
   ```
   # Twilio Credentials
   TWILIO_ACCOUNT_SID=your_twilio_account_sid
   TWILIO_AUTH_TOKEN=your_twilio_auth_token
   TWILIO_PHONE_NUMBER=your_twilio_phone_number
   TWILIO_APP_SID=your_twilio_app_sid  # Optional: Only needed if using TwiML apps

   # Google Cloud Credentials
   GOOGLE_APPLICATION_CREDENTIALS=path/to/your/service-account-key.json
   GOOGLE_PROJECT_ID=your_google_project_id

   # Gemini API Key
   GEMINI_API_KEY=your_gemini_api_key  # Get this from Google AI Studio

   # Server Configuration
   PORT=8080
   ```

4. Run the application:
   ```bash
   go run main.go
   ```

5. Expose your local server using a tool like ngrok:
   ```bash
   ngrok http 8080
   ```

6. Configure your Twilio phone number's webhook to point to your ngrok URL + `/twilio/call`

## Usage

1. Call your Twilio phone number
2. Speak naturally with the AI therapist
3. Receive empathetic, supportive responses

## Development

### Running Tests

```bash
go test ./...
```

### Deployment

The application can be deployed to any platform that supports Go applications, such as:

- Google Cloud Run
- AWS Lambda with API Gateway
- Heroku
- Digital Ocean

## License

This project is licensed under the MIT License - see the LICENSE file for details.
