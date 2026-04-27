package main

import (
	"context"
	"errors"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/router"
	"github.com/kombifyio/SpeechKit/internal/serverclient"
	"github.com/kombifyio/SpeechKit/internal/stt"
)

// stubSTT implements stt.STTProvider for the composite-transcriber tests.
type stubSTT struct {
	result *stt.Result
	err    error
	called int
}

func (s *stubSTT) Transcribe(_ context.Context, _ []byte, _ stt.TranscribeOpts) (*stt.Result, error) {
	s.called++
	return s.result, s.err
}
func (s *stubSTT) Name() string                   { return "stub" }
func (s *stubSTT) Health(_ context.Context) error { return nil }

// stubLocal implements the Transcriber interface (Route signature) so the
// composite can be exercised without spinning up a real router.
type stubLocal struct {
	result *stt.Result
	err    error
	called int
}

func (l *stubLocal) Route(_ context.Context, _ []byte, _ float64, _ stt.TranscribeOpts) (*stt.Result, error) {
	l.called++
	return l.result, l.err
}

func TestBuildServerDelegatesAllLocal(t *testing.T) {
	cfg := &config.Config{
		ModelSelection: config.BuiltInPrimaryModelSelectionDefaults(),
	}
	d, err := buildServerDelegates(cfg)
	if err != nil {
		t.Fatalf("buildServerDelegates: %v", err)
	}
	if d != nil {
		t.Fatalf("expected nil delegates when every mode is local, got %+v", d)
	}
}

func TestBuildServerDelegatesDisabledConnection(t *testing.T) {
	cfg := &config.Config{}
	cfg.ModelSelection.Dictate.ModeSource = config.ModeSourceServer
	cfg.ServerConnection = config.ServerConnectionConfig{Enabled: false}

	d, err := buildServerDelegates(cfg)
	if err != nil {
		t.Fatalf("buildServerDelegates: %v", err)
	}
	if d != nil {
		t.Fatalf("expected nil when mode wants server but ServerConnection disabled, got %+v", d)
	}
}

func TestBuildServerDelegatesActive(t *testing.T) {
	t.Setenv("SC_TEST_DELEGATE_TOKEN", "secret")
	cfg := &config.Config{}
	cfg.ModelSelection.Dictate.ModeSource = config.ModeSourceServer
	cfg.ModelSelection.Assist.ModeSource = config.ModeSourceLocal
	cfg.ModelSelection.VoiceAgent.ModeSource = config.ModeSourceServer
	cfg.ServerConnection = config.ServerConnectionConfig{
		Enabled:         true,
		URL:             "http://localhost:8080",
		BearerTokenEnv:  "SC_TEST_DELEGATE_TOKEN",
		FallbackToLocal: true,
	}

	d, err := buildServerDelegates(cfg)
	if err != nil {
		t.Fatalf("buildServerDelegates: %v", err)
	}
	if d == nil {
		t.Fatal("delegates = nil, want non-nil")
	}
	if !d.hasDictation() {
		t.Error("hasDictation = false")
	}
	if d.hasAssist() {
		t.Error("hasAssist = true, want false (mode is local)")
	}
	if !d.hasVoiceAgent() {
		t.Error("hasVoiceAgent = false")
	}
	if !d.shouldFallback() {
		t.Error("shouldFallback = false")
	}
}

func TestDictationTranscriberPicksLocal(t *testing.T) {
	r := &router.Router{Strategy: router.StrategyDynamic}
	got := dictationTranscriber(r, nil)
	if got != Transcriber(r) {
		t.Errorf("expected raw router when delegates nil, got %T", got)
	}
	// Even with a non-nil but Dictation-less delegate, returns raw router.
	d := &serverDelegates{}
	got = dictationTranscriber(r, d)
	if got != Transcriber(r) {
		t.Errorf("expected raw router when delegates.hasDictation() false, got %T", got)
	}
}

func TestDictationTranscriberPicksComposite(t *testing.T) {
	t.Setenv("SC_TEST_COMP_TOKEN", "ok")
	cfg := &config.Config{}
	cfg.ModelSelection.Dictate.ModeSource = config.ModeSourceServer
	cfg.ServerConnection = config.ServerConnectionConfig{
		Enabled:        true,
		URL:            "http://localhost:8080",
		BearerTokenEnv: "SC_TEST_COMP_TOKEN",
	}
	d, err := buildServerDelegates(cfg)
	if err != nil {
		t.Fatalf("buildServerDelegates: %v", err)
	}
	r := &router.Router{Strategy: router.StrategyDynamic}
	got := dictationTranscriber(r, d)
	if _, ok := got.(*compositeTranscriber); !ok {
		t.Errorf("expected *compositeTranscriber, got %T", got)
	}
}

func TestCompositeTranscriberServerSuccess(t *testing.T) {
	server := &stubSTT{result: &stt.Result{Text: "from server"}}
	local := &stubLocal{result: &stt.Result{Text: "from local"}}
	c := newCompositeTranscriber(server, local, true)
	res, err := c.Route(context.Background(), []byte{0x01}, 1.0, stt.TranscribeOpts{})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if res.Text != "from server" {
		t.Errorf("text = %q, want from server", res.Text)
	}
	if local.called != 0 {
		t.Errorf("local called %d times, want 0", local.called)
	}
}

func TestCompositeTranscriberFallbackOnNetworkError(t *testing.T) {
	server := &stubSTT{err: errors.New("connection refused")}
	local := &stubLocal{result: &stt.Result{Text: "from local"}}
	c := newCompositeTranscriber(server, local, true)
	res, err := c.Route(context.Background(), []byte{0x01}, 1.0, stt.TranscribeOpts{})
	if err != nil {
		t.Fatalf("Route: %v", err)
	}
	if res.Text != "from local" {
		t.Errorf("text = %q, want from local", res.Text)
	}
	if local.called != 1 {
		t.Errorf("local called %d times, want 1", local.called)
	}
}

func TestCompositeTranscriberNoFallbackOnAppError(t *testing.T) {
	server := &stubSTT{err: &serverclient.ServerError{Status: 503, Code: "provider_unavailable", Message: "oops"}}
	local := &stubLocal{result: &stt.Result{Text: "from local"}}
	c := newCompositeTranscriber(server, local, true)
	_, err := c.Route(context.Background(), []byte{0x01}, 1.0, stt.TranscribeOpts{})
	if err == nil {
		t.Fatal("err = nil, want ServerError")
	}
	if !serverclient.IsCode(err, "provider_unavailable") {
		t.Errorf("err = %v, want IsCode(provider_unavailable)", err)
	}
	if local.called != 0 {
		t.Errorf("local called %d times, want 0 (app error must not fall back)", local.called)
	}
}

func TestCompositeTranscriberFallbackDisabled(t *testing.T) {
	server := &stubSTT{err: errors.New("connection refused")}
	local := &stubLocal{result: &stt.Result{Text: "from local"}}
	c := newCompositeTranscriber(server, local, false)
	_, err := c.Route(context.Background(), []byte{0x01}, 1.0, stt.TranscribeOpts{})
	if err == nil || !errors.Is(err, err) {
		t.Fatal("expected error to propagate when FallbackToLocal=false")
	}
	if local.called != 0 {
		t.Errorf("local called %d times, want 0 when fallback disabled", local.called)
	}
}
