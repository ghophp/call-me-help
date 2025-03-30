# Test Data

This directory contains test data files for integration tests.

## Audio Files

- `test_audio.raw`: Sample 8kHz mulaw encoded audio for speech-to-text testing.

To generate test audio files, you can use Twilio Media Streams or record audio with the following characteristics:
- 8kHz sampling rate
- mulaw encoding
- mono channel

## Running Integration Tests

To run integration tests that use these files, use:

```bash
INTEGRATION_TESTS=true go test -v ./services/...
```

Make sure your Google Cloud credentials are properly set up before running integration tests. 