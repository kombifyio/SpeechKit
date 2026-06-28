//go:build integration

package stt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/testutil"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

const speakerIntegrationTimeout = 4 * time.Minute

func TestIntegrationDeepgramDiarization(t *testing.T) {
	key := testutil.RequireEnvOrSkip(t, envDeepgramAPIKey, "Deepgram speaker diarization integration requires live Deepgram credentials.")
	audio := loadSpeakerIntegrationAudio(t)

	ctx, cancel := context.WithTimeout(context.Background(), speakerIntegrationTimeout)
	defer cancel()

	result, err := NewDeepgramProvider(key, "nova-3").Transcribe(ctx, audio, speakerIntegrationOptions())
	if err != nil {
		t.Fatalf("Deepgram Transcribe: %v", err)
	}
	assertTwoSpeakerDiarization(t, result)
}

func TestIntegrationAssemblyAIDiarization(t *testing.T) {
	key := testutil.RequireEnvOrSkip(t, envAssemblyAIAPIKey, "AssemblyAI speaker diarization integration requires live AssemblyAI credentials.")
	audio := loadSpeakerIntegrationAudio(t)

	ctx, cancel := context.WithTimeout(context.Background(), speakerIntegrationTimeout)
	defer cancel()

	provider := NewAssemblyAIProvider(key, "universal-3-pro,universal-2")
	provider.PollTimeout = speakerIntegrationTimeout
	result, err := provider.Transcribe(ctx, audio, speakerIntegrationOptions())
	if err != nil {
		t.Fatalf("AssemblyAI Transcribe: %v", err)
	}
	assertTwoSpeakerDiarization(t, result)
}

func TestIntegrationGoogleSTTDiarization(t *testing.T) {
	key := testutil.RequireAnyEnvOrSkip(t, []string{envGoogleSTTAPIKey, envGoogleCloudSTTAPIKey, envGoogleLegacySTTAPIKey}, "Google STT diarization integration requires live Google STT credentials.")
	audio := loadSpeakerIntegrationAudio(t)

	ctx, cancel := context.WithTimeout(context.Background(), speakerIntegrationTimeout)
	defer cancel()

	result, err := NewGoogleSTTProvider(key, "latest_long").Transcribe(ctx, audio, speakerIntegrationOptions())
	if err != nil {
		t.Fatalf("Google STT Transcribe: %v", err)
	}
	assertTwoSpeakerDiarization(t, result)
}

func TestIntegrationDeepgramSpeakerStreaming(t *testing.T) {
	key := testutil.RequireEnvOrSkip(t, envDeepgramAPIKey, "Deepgram speaker streaming integration requires live Deepgram credentials.")
	audio, format := loadSpeakerIntegrationPCM(t)

	ctx, cancel := context.WithTimeout(context.Background(), speakerIntegrationTimeout)
	defer cancel()

	opts := speakerIntegrationOptions().Speaker
	opts.PreferStreaming = true
	stream, err := NewDeepgramProvider(key, "nova-3").StartSpeakerStream(ctx, opts, format)
	if err != nil {
		t.Fatalf("Deepgram StartSpeakerStream: %v", err)
	}
	assertSpeakerStreamTwoSpeakers(t, ctx, stream, audio, format)
}

func TestIntegrationAssemblyAISpeakerStreaming(t *testing.T) {
	key := testutil.RequireEnvOrSkip(t, envAssemblyAIAPIKey, "AssemblyAI speaker streaming integration requires live AssemblyAI credentials.")
	audio, format := loadSpeakerIntegrationPCM(t)

	ctx, cancel := context.WithTimeout(context.Background(), speakerIntegrationTimeout)
	defer cancel()

	opts := speakerIntegrationOptions().Speaker
	opts.PreferStreaming = true
	provider := NewAssemblyAIProvider(key, "universal-3-pro,universal-2")
	provider.StreamingModel = "universal-3-5-pro"
	stream, err := provider.StartSpeakerStream(ctx, opts, format)
	if err != nil {
		t.Fatalf("AssemblyAI StartSpeakerStream: %v", err)
	}
	assertSpeakerStreamTwoSpeakers(t, ctx, stream, audio, format)
}

func speakerIntegrationOptions() TranscribeOpts {
	language := speakerIntegrationLanguage()
	return TranscribeOpts{
		Language: language,
		Speaker: speaker.Options{
			Enabled:             true,
			Diarization:         true,
			Language:            language,
			SpeakersExpected:    2,
			MinSpeakersExpected: 2,
			MaxSpeakersExpected: 2,
		},
	}
}

func speakerIntegrationLanguage() string {
	if language := strings.TrimSpace(os.Getenv("SPEECHKIT_SPEAKER_TEST_LANGUAGE")); language != "" {
		return language
	}
	return "de"
}

func loadSpeakerIntegrationAudio(t *testing.T) []byte {
	t.Helper()
	for _, envName := range []string{"SPEECHKIT_SPEAKER_TEST_AUDIO", "SPEECHKIT_E2E_DICTATION_AUDIO"} {
		path := strings.TrimSpace(os.Getenv(envName))
		if path == "" {
			continue
		}
		audio, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s=%s: %v", envName, path, err)
		}
		if len(audio) == 0 {
			t.Fatalf("%s=%s is empty", envName, path)
		}
		return audio
	}
	if raw := strings.TrimSpace(os.Getenv("SPEECHKIT_SPEAKER_TEST_AUDIO_B64")); raw != "" {
		audio, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			t.Fatalf("decode SPEECHKIT_SPEAKER_TEST_AUDIO_B64: %v", err)
		}
		if len(audio) == 0 {
			t.Fatal("SPEECHKIT_SPEAKER_TEST_AUDIO_B64 decoded to empty audio")
		}
		return audio
	}
	testutil.SkipOrFailMissingConfig(t, "SPEECHKIT_SPEAKER_TEST_AUDIO or SPEECHKIT_E2E_DICTATION_AUDIO or SPEECHKIT_SPEAKER_TEST_AUDIO_B64", "Speaker integration tests require a spoken audio fixture.")
	return nil
}

func loadSpeakerIntegrationPCM(t *testing.T) ([]byte, speaker.AudioFormat) {
	t.Helper()
	audio := loadSpeakerIntegrationAudio(t)
	pcm, format, err := speakerIntegrationPCMFromAudio(audio)
	if err != nil {
		t.Fatalf("speaker streaming fixture must be raw PCM16, 16-bit PCM WAV, or 32-bit float WAV: %v", err)
	}
	return pcm, format
}

func speakerIntegrationPCMFromAudio(audio []byte) ([]byte, speaker.AudioFormat, error) {
	format := speaker.AudioFormat{
		Encoding:     speaker.AudioEncodingLinear16,
		SampleRateHz: 16000,
		Channels:     1,
		Container:    "raw",
	}
	if !bytes.HasPrefix(audio, []byte("RIFF")) {
		return audio, format, nil
	}
	if len(audio) < 12 || !bytes.Equal(audio[8:12], []byte("WAVE")) {
		return nil, format, errors.New("RIFF file is not WAVE")
	}

	var data []byte
	var float32WAV bool
	var foundFormat bool
	offset := 12
	for offset+8 <= len(audio) {
		chunkID := string(audio[offset : offset+4])
		chunkSize := int(binary.LittleEndian.Uint32(audio[offset+4 : offset+8]))
		offset += 8
		if chunkSize < 0 || offset+chunkSize > len(audio) {
			return nil, format, errors.New("invalid WAV chunk size")
		}
		chunk := audio[offset : offset+chunkSize]
		switch chunkID {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, format, errors.New("WAV fmt chunk is too short")
			}
			audioFormat := binary.LittleEndian.Uint16(chunk[0:2])
			isPCM := audioFormat == 1
			if audioFormat == 0xfffe && len(chunk) >= 40 {
				isPCM = binary.LittleEndian.Uint16(chunk[24:26]) == 1
			}
			bitsPerSample := binary.LittleEndian.Uint16(chunk[14:16])
			switch {
			case isPCM && bitsPerSample == 16:
			case audioFormat == 3 && bitsPerSample == 32:
				float32WAV = true
			default:
				return nil, format, errors.New("WAV format is not supported for speaker streaming")
			}
			format.SampleRateHz = int(binary.LittleEndian.Uint32(chunk[4:8]))
			format.Channels = int(binary.LittleEndian.Uint16(chunk[2:4]))
			foundFormat = true
		case "data":
			data = chunk
		}
		offset += chunkSize
		if chunkSize%2 == 1 {
			offset++
		}
	}
	if !foundFormat {
		return nil, format, errors.New("WAV fmt chunk missing")
	}
	if len(data) == 0 {
		return nil, format, errors.New("WAV data chunk missing or empty")
	}
	if float32WAV {
		pcm, err := float32WAVToPCM16(data)
		if err != nil {
			return nil, format, err
		}
		data = pcm
	}
	return data, format.Normalized(), nil
}

func float32WAVToPCM16(data []byte) ([]byte, error) {
	if len(data)%4 != 0 {
		return nil, errors.New("float WAV data is not aligned to 32-bit samples")
	}
	pcm := make([]byte, len(data)/2)
	for in, out := 0, 0; in < len(data); in, out = in+4, out+2 {
		sample := math.Float32frombits(binary.LittleEndian.Uint32(data[in : in+4]))
		if math.IsNaN(float64(sample)) {
			sample = 0
		}
		if sample > 1 {
			sample = 1
		} else if sample < -1 {
			sample = -1
		}
		binary.LittleEndian.PutUint16(pcm[out:out+2], uint16(int16(math.Round(float64(sample)*32767))))
	}
	return pcm, nil
}

func assertTwoSpeakerDiarization(t *testing.T, result *Result) {
	t.Helper()
	if result == nil {
		t.Fatal("result is nil")
	}
	if strings.TrimSpace(result.Text) == "" {
		t.Fatal("result text is empty")
	}
	if result.Speakers == nil {
		t.Fatal("speakers result is nil")
	}
	if len(result.Speakers.Segments) == 0 {
		t.Fatal("speaker segments are empty")
	}
	labels := map[string]bool{}
	for _, segment := range result.Speakers.Segments {
		if segment.SpeakerLabel != "" {
			labels[segment.SpeakerLabel] = true
		}
	}
	if len(labels) < 2 {
		t.Fatalf("speaker labels = %#v, want at least two labels", labels)
	}
}

func assertSpeakerStreamTwoSpeakers(t *testing.T, ctx context.Context, stream speaker.SpeakerStream, audio []byte, format speaker.AudioFormat) {
	t.Helper()
	defer stream.Close()

	sendErr := make(chan error, 1)
	go func() {
		sendErr <- sendSpeakerIntegrationAudio(ctx, stream, audio, format)
	}()

	labels := map[string]bool{}
	frames := 0
	finalFrames := 0
	senderDone := false
	for {
		if !senderDone {
			select {
			case err := <-sendErr:
				senderDone = true
				if err != nil {
					t.Fatalf("send streaming audio: %v", err)
				}
			default:
			}
		}
		receiveCtx := ctx
		cancelReceive := func() {}
		if senderDone && finalFrames > 0 {
			receiveCtx, cancelReceive = context.WithTimeout(ctx, 10*time.Second)
		}
		frame, err := stream.Receive(receiveCtx)
		cancelReceive()
		if err != nil {
			if senderDone && finalFrames > 0 && errors.Is(err, context.DeadlineExceeded) {
				break
			}
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("Receive: %v", err)
		}
		if frame == nil {
			continue
		}
		frames++
		if frame.IsFinal {
			finalFrames++
		}
		collectSpeakerLabels(labels, frame)
		if len(labels) >= 2 && finalFrames > 0 {
			return
		}
		if senderDone && finalFrames > 0 && frames >= 3 {
			break
		}
	}
	if !senderDone {
		if err := <-sendErr; err != nil {
			t.Fatalf("send streaming audio: %v", err)
		}
	}
	if frames == 0 {
		t.Fatal("speaker stream produced no frames")
	}
	if finalFrames == 0 {
		t.Fatalf("speaker stream produced %d frames but no final frame", frames)
	}
	if len(labels) < 2 {
		t.Fatalf("speaker stream labels = %#v, want at least two labels", labels)
	}
}

func sendSpeakerIntegrationAudio(ctx context.Context, stream speaker.SpeakerStream, audio []byte, format speaker.AudioFormat) error {
	format = format.Normalized()
	bytesPerSecond := format.SampleRateHz * format.Channels * 2
	chunkSize := bytesPerSecond / 10
	if chunkSize < 3200 {
		chunkSize = 3200
	}
	for offset := 0; offset < len(audio); offset += chunkSize {
		end := offset + chunkSize
		if end > len(audio) {
			end = len(audio)
		}
		if err := stream.SendAudio(ctx, audio[offset:end]); err != nil {
			return err
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return stream.EndAudio(ctx)
}

func collectSpeakerLabels(labels map[string]bool, frame *speaker.SpeakerFrame) {
	if frame.Segment != nil && frame.Segment.SpeakerLabel != "" {
		labels[frame.Segment.SpeakerLabel] = true
	}
	for _, word := range frame.Words {
		if word.SpeakerLabel != "" {
			labels[word.SpeakerLabel] = true
		}
	}
	for _, entry := range frame.Speakers {
		if entry.Label != "" {
			labels[entry.Label] = true
		}
	}
}

// TestIntegrationGoogleSTTStreamingTranscription gates Google STT v2
// StreamingRecognize. v2 streaming cannot diarize (Chirp-3 docs), so this
// proves realtime TRANSCRIPTION only: transcript text must arrive and frames
// must carry no speaker labels. Requires service-account/ADC credentials.
func TestIntegrationGoogleSTTStreamingTranscription(t *testing.T) {
	if !googleStreamingCredentialsPresent(envGoogleApplicationCreds, envGoogleSTTCredentialsJSON) {
		testutil.SkipOrFailMissingConfig(t, envGoogleSTTCredentialsJSON+" or "+envGoogleApplicationCreds, "Google STT streaming integration requires service-account or ADC credentials.")
	}
	audio, format := loadSpeakerIntegrationPCM(t)

	ctx, cancel := context.WithTimeout(context.Background(), speakerIntegrationTimeout)
	defer cancel()

	opts := speakerIntegrationOptions().Speaker
	opts.PreferStreaming = true
	stream, err := NewGoogleSTTProvider("", "latest_long").StartSpeakerStream(ctx, opts, format)
	if err != nil {
		t.Fatalf("Google StartSpeakerStream: %v", err)
	}
	assertSpeakerStreamHasTranscript(t, ctx, stream, audio, format)
}

func assertSpeakerStreamHasTranscript(t *testing.T, ctx context.Context, stream speaker.SpeakerStream, audio []byte, format speaker.AudioFormat) {
	t.Helper()
	defer stream.Close()

	sendErr := make(chan error, 1)
	go func() {
		sendErr <- sendSpeakerIntegrationAudio(ctx, stream, audio, format)
	}()

	var transcript strings.Builder
	frames := 0
	finalFrames := 0
	senderDone := false
	for {
		if !senderDone {
			select {
			case err := <-sendErr:
				senderDone = true
				if err != nil {
					t.Fatalf("send streaming audio: %v", err)
				}
			default:
			}
		}
		receiveCtx := ctx
		cancelReceive := func() {}
		if senderDone {
			receiveCtx, cancelReceive = context.WithTimeout(ctx, 10*time.Second)
		}
		frame, err := stream.Receive(receiveCtx)
		cancelReceive()
		if err != nil {
			if senderDone && errors.Is(err, context.DeadlineExceeded) {
				break
			}
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("Receive: %v", err)
		}
		if frame == nil {
			continue
		}
		frames++
		if text := strings.TrimSpace(frame.Text); text != "" {
			if transcript.Len() > 0 {
				transcript.WriteByte(' ')
			}
			transcript.WriteString(text)
		}
		if frame.IsFinal {
			finalFrames++
		}
		// v2 streaming is transcription-only: frames must carry no speaker labels.
		if frame.Segment != nil && frame.Segment.SpeakerLabel != "" {
			t.Fatalf("google v2 streaming must be transcription-only, got speaker label %q", frame.Segment.SpeakerLabel)
		}
		if senderDone && finalFrames > 0 && transcript.Len() > 0 {
			break
		}
	}
	if !senderDone {
		if err := <-sendErr; err != nil {
			t.Fatalf("send streaming audio: %v", err)
		}
	}
	if frames == 0 {
		t.Fatal("streaming produced no frames")
	}
	if strings.TrimSpace(transcript.String()) == "" {
		t.Fatalf("streaming produced %d frames but no transcript text", frames)
	}
	t.Logf("google v2 streaming transcript (%d frames, %d final): %q", frames, finalFrames, transcript.String())
}
