# TTS Audio Saving Feature

This feature enables the automatic saving of all Text-to-Speech (TTS) generated audio content to disk as it's played on the output channel during calls.

## Overview

When the system generates audio responses through the TTS service (Google Cloud Text-to-Speech), it now automatically saves this audio data to files on disk. This allows for:

- Archiving all spoken responses for later review
- Quality assessment of the TTS output
- Training data collection
- Debugging voice quality issues

## Configuration

The audio saving feature is configurable through the following environment variable:

- `AUDIO_OUTPUT_DIR`: Directory where audio files will be saved (defaults to `saved_audio` if not specified)

Example configuration in your `.env` file:

```
AUDIO_OUTPUT_DIR=/var/data/call-me-help/audio
```

## File Format

Audio files are saved in μ-law format (8-bit, 8kHz mono), which is the same format used for telephony in Twilio. The files use the following naming convention:

```
{callSID}_{timestamp}_{text}.raw
```

Where:
- `callSID`: The Twilio Call SID
- `timestamp`: Timestamp in format YYYYMMDD-HHMMSS.sss
- `text`: A shortened, sanitized version of the TTS text (first 30 characters)

Example filename:
```
CA9e5a93cab82e4f6ea42c83172bc8a59b_20230415-143027.892_Hello_Im_here_to_help_you.raw
```

## API Endpoints

Two new API endpoints have been added to interact with the saved audio files:

### List Audio Files

```
GET /audio
```

Returns a JSON array of audio file metadata:

```json
[
  {
    "filename": "CA9e5a93cab82e4f6ea42c83172bc8a59b_20230415-143027.892_Hello_Im_here_to_help_you.raw",
    "callSid": "CA9e5a93cab82e4f6ea42c83172bc8a59b",
    "timestamp": "2023-04-15T14:30:27.892Z",
    "text": "Hello_Im_here_to_help_you",
    "sizeBytes": 12500,
    "downloadUrl": "http://localhost:8080/audio/download/CA9e5a93cab82e4f6ea42c83172bc8a59b_20230415-143027.892_Hello_Im_here_to_help_you.raw"
  },
  ...
]
```

### Download Audio File

```
GET /audio/download/{filename}
```

Downloads the raw audio file. To play these files, you may need a player that supports μ-law format, or convert them to WAV/MP3.

Example conversion using ffmpeg:

```bash
ffmpeg -f mulaw -ar 8000 -ac 1 -i input.raw output.wav
```

## Implementation Details

- Audio files are saved immediately after TTS generation, before being sent to the output channel
- Saving operates asynchronously and doesn't block the main call flow
- If saving fails for any reason, the error is logged but doesn't impact the call
- Files are never automatically deleted - implement your own retention policy as needed 