package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/audio"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// The example's host seam lets us exercise ownership without a microphone,
// downloaded model or subprocess. Cleanup is the stable invariant under test.
func TestDictationStarterOwnsResources(t *testing.T) {
	failure := errors.New("operation failed")
	for _, scenario := range []string{"success", "startup failure", "capture failure", "cancel capture", "transcribe failure", "close failure"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			provider := &testProvider{}
			input := &testRecorder{pcm: make([]byte, audio.SampleRate*audio.BytesPerSample)}
			opts := options{language: "de", timeout: time.Second}
			wantErr := error(nil)
			switch scenario {
			case "startup failure":
				provider.startErr = failure
				wantErr = failure
			case "capture failure":
				input.startErr = failure
				wantErr = failure
			case "cancel capture":
				input.onStart = cancel
				opts.recordFor = time.Minute
				wantErr = context.Canceled
			case "transcribe failure":
				provider.transcribeErr = failure
				wantErr = failure
			case "close failure":
				input.closeErr = failure
				wantErr = failure
			}
			opened := false
			result, err := dictate(ctx, opts, provider, func() (recorder, error) {
				opened = true
				return input, nil
			}, io.Discard)
			if !errors.Is(err, wantErr) {
				t.Fatalf("completion error = %v, want %v", err, wantErr)
			}
			if !provider.stopped || (opened && !input.closed) {
				t.Fatal("owned provider or recording input was left open")
			}
			if scenario == "startup failure" && opened {
				t.Fatal("microphone was opened before the provider was ready")
			}
			if scenario == "cancel capture" && provider.transcribed {
				t.Fatal("canceled capture was sent for transcription")
			}
			if scenario == "success" {
				if result.Transcript.Text != "Guten Tag" || result.Transcript.Language != "de" {
					t.Fatalf("provider result was not returned: %#v", result.Transcript)
				}
				if provider.processCtx.Err() != nil {
					t.Fatal("the per-request deadline canceled the provider lifetime")
				}
			}
		})
	}
}

func TestStarterRequiresExplicitInput(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"-model", "ggml-base.bin"},
		{"-model", "ggml-base.bin", "-wav", "input.wav", "-record-for", "5s"},
		{"-model", "ggml-base.bin", "-record-for", "-1s"},
	} {
		if _, err := parseOptions(args, io.Discard); err == nil {
			t.Fatalf("ambiguous input accepted: %v", args)
		}
	}
}

// File input must not silently relabel unsupported audio as 16 kHz mono.
func TestStarterAcceptsOnlySupportedWAV(t *testing.T) {
	pcm := make([]byte, audio.SampleRate*audio.BytesPerSample)
	wavWithFormat := func(rate uint32, channels, bits uint16) []byte {
		wav := audio.PCMToWAV(pcm)
		binary.LittleEndian.PutUint16(wav[22:], channels)
		binary.LittleEndian.PutUint32(wav[24:], rate)
		binary.LittleEndian.PutUint32(wav[28:], rate*uint32(channels)*uint32(bits/8))
		binary.LittleEndian.PutUint16(wav[32:], channels*(bits/8))
		binary.LittleEndian.PutUint16(wav[34:], bits)
		return wav
	}
	for _, scenario := range []struct {
		name string
		wav  []byte
		ok   bool
	}{
		{"supported", wavWithFormat(16000, 1, 16), true},
		{"wrong rate", wavWithFormat(48000, 1, 16), false},
		{"stereo", wavWithFormat(16000, 2, 16), false},
		{"wrong sample format", wavWithFormat(16000, 1, 8), false},
		{"not WAV", pcm, false},
		{"truncated", []byte("RIFF"), false},
		{"too short", audio.PCMToWAV([]byte{0, 0}), false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input.wav")
			if err := os.WriteFile(path, scenario.wav, 0600); err != nil {
				t.Fatal(err)
			}
			got, err := readWAV(path)
			if scenario.ok {
				if err != nil || !bytes.Equal(got, pcm) {
					t.Fatalf("supported input was not preserved: %v", err)
				}
			} else if err == nil {
				t.Fatal("unsupported input was accepted")
			}
		})
	}
}

type testProvider struct {
	processCtx    context.Context
	startErr      error
	transcribeErr error
	stopped       bool
	transcribed   bool
}

func (p *testProvider) StartServer(ctx context.Context) error { p.processCtx = ctx; return p.startErr }
func (p *testProvider) StopServer()                           { p.stopped = true }
func (*testProvider) Name() string                            { return "local" }
func (*testProvider) Health(ctx context.Context) error        { return ctx.Err() }
func (p *testProvider) Transcribe(ctx context.Context, wav []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	p.transcribed = true
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pcm, rate, channels, ok := stt.PCM16FromWAV(wav)
	if !ok || rate != audio.SampleRate || channels != audio.Channels || len(pcm) < speechkit.DefaultMinPCMBytes {
		return nil, errors.New("provider did not receive usable audio")
	}
	if p.transcribeErr != nil {
		return nil, p.transcribeErr
	}
	return &stt.Result{Text: "Guten Tag", Language: opts.Language, Provider: "local"}, nil
}

type testRecorder struct {
	pcm      []byte
	onStart  func()
	startErr error
	closeErr error
	closed   bool
}

func (r *testRecorder) Start() error {
	if r.onStart != nil {
		r.onStart()
	}
	return r.startErr
}
func (r *testRecorder) Stop() ([]byte, error)    { return r.pcm, nil }
func (*testRecorder) SetPCMHandler(func([]byte)) {}
func (r *testRecorder) Close() error             { r.closed = true; return r.closeErr }
