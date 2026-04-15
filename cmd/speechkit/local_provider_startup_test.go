package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

type retryLocalProvider struct {
	attempts  int
	succeedAt int
	ready     bool
	installOK bool
}

func (p *retryLocalProvider) StartServer(context.Context) error {
	p.attempts++
	if p.attempts < p.succeedAt {
		return errors.New("startup failed")
	}
	p.ready = true
	return nil
}

func (p *retryLocalProvider) IsReady() bool {
	return p.ready
}

func (p *retryLocalProvider) VerifyInstallation() stt.InstallStatus {
	if p.installOK {
		return stt.InstallStatus{
			BinaryFound: true,
			BinaryPath:  "/fake/whisper-server",
			ModelFound:  true,
			ModelPath:   "/fake/models/ggml-small.bin",
			ModelBytes:  484_264_096,
		}
	}
	return stt.InstallStatus{
		Problems: []string{"whisper-server binary not found"},
	}
}

func (p *retryLocalProvider) Transcribe(context.Context, []byte, stt.TranscribeOpts) (*stt.Result, error) {
	return &stt.Result{Provider: "local"}, nil
}

func (p *retryLocalProvider) Name() string {
	return "local"
}

func (p *retryLocalProvider) Health(context.Context) error {
	if p.ready {
		return nil
	}
	return errors.New("not ready")
}

func TestStartLocalProviderWithRetryRecovers(t *testing.T) {
	previousDelay := localProviderRetryDelay
	localProviderRetryDelay = time.Millisecond
	defer func() { localProviderRetryDelay = previousDelay }()

	provider := &retryLocalProvider{succeedAt: 2, installOK: true}
	r := &router.Router{}
	r.SetLocal(provider)
	state := &appState{}

	startLocalProviderWithRetry(context.Background(), state, r, provider, 3)

	if provider.attempts != 2 {
		t.Fatalf("attempts = %d, want 2", provider.attempts)
	}
	if len(state.providers) != 1 || state.providers[0] != "local" {
		t.Fatalf("providers = %v, want [local]", state.providers)
	}
}

func TestStartLocalProviderWithRetryExhaustsAttempts(t *testing.T) {
	previousDelay := localProviderRetryDelay
	localProviderRetryDelay = time.Millisecond
	defer func() { localProviderRetryDelay = previousDelay }()

	provider := &retryLocalProvider{succeedAt: 10, installOK: true}
	r := &router.Router{}
	r.SetLocal(provider)
	state := &appState{}

	startLocalProviderWithRetry(context.Background(), state, r, provider, 2)

	if provider.attempts != 2 {
		t.Fatalf("attempts = %d, want 2", provider.attempts)
	}
	if len(state.providers) != 0 {
		t.Fatalf("providers = %v, want []", state.providers)
	}

	foundFailure := false
	for _, entry := range state.logEntries {
		if entry.Message == "Local STT unavailable after retries" {
			foundFailure = true
			break
		}
	}
	if !foundFailure {
		t.Fatalf("expected failure log entry, logs=%v", state.logEntries)
	}
}

func TestStartLocalProviderWithRetryAbortsMissingBinary(t *testing.T) {
	previousDelay := localProviderRetryDelay
	localProviderRetryDelay = time.Millisecond
	defer func() { localProviderRetryDelay = previousDelay }()

	provider := &retryLocalProvider{succeedAt: 1, installOK: false}
	r := &router.Router{}
	r.SetLocal(provider)
	state := &appState{}

	startLocalProviderWithRetry(context.Background(), state, r, provider, 4)

	// Should NOT attempt startup at all when binary is missing.
	if provider.attempts != 0 {
		t.Fatalf("attempts = %d, want 0 (should abort before trying)", provider.attempts)
	}

	foundAbort := false
	for _, entry := range state.logEntries {
		if entry.Type == "error" {
			foundAbort = true
			break
		}
	}
	if !foundAbort {
		t.Fatalf("expected error log entry for missing binary, logs=%v", state.logEntries)
	}
}
