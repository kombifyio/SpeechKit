package main

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/router"
)

// Regression: 2026-05-19. app_init.go forces cfg.Routing.Strategy = "local-only"
// on startup whenever the dictate primary is a local-builtin STT. That value
// is persisted to config.toml on every save, so once it lands it survives the
// app session. When the user later switches the dictate primary to Hugging
// Face (or any cloud provider) the picker only updates ModelSelection.Dictate
// — Routing.Strategy stays "local-only", the router rejects every cloud
// provider, and dictation silently keeps hitting whisper-server local.
// syncConfiguredSTTRouter is on the hot path for every settings save, so the
// reconciliation lives there.
func TestSyncConfiguredSTTRouterReconcilesStrategyWhenPickerSwitchesAwayFromLocal(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.ModelSelection.Dictate.PrimaryProfileID = "stt.huggingface.whisper-large-v3"
	cfg.Routing.Strategy = "local-only" // stale value from a prior local-builtin selection
	cfg.HuggingFace.Enabled = true
	cfg.HuggingFace.Model = "openai/whisper-large-v3"

	r := &router.Router{Strategy: router.StrategyLocalOnly}
	syncConfiguredSTTRouter(context.Background(), cfg, nil, r)

	if cfg.Routing.Strategy != "dynamic" {
		t.Fatalf("cfg.Routing.Strategy = %q, want %q after switching dictate primary away from local-builtin", cfg.Routing.Strategy, "dynamic")
	}
	if r.Strategy != router.StrategyDynamic {
		t.Fatalf("router.Strategy = %q, want %q", r.Strategy, router.StrategyDynamic)
	}
}

func TestSyncConfiguredSTTRouterForcesLocalOnlyWhenPickerSelectsLocalBuiltIn(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.ModelSelection.Dictate.PrimaryProfileID = "stt.local.whispercpp"
	cfg.Routing.Strategy = "dynamic" // start in non-local mode

	r := &router.Router{Strategy: router.StrategyDynamic}
	syncConfiguredSTTRouter(context.Background(), cfg, nil, r)

	if cfg.Routing.Strategy != "local-only" {
		t.Fatalf("cfg.Routing.Strategy = %q, want %q when dictate primary is local-builtin", cfg.Routing.Strategy, "local-only")
	}
	if r.Strategy != router.StrategyLocalOnly {
		t.Fatalf("router.Strategy = %q, want %q", r.Strategy, router.StrategyLocalOnly)
	}
}

// Regression: 2026-05-19. Even after R4 reconciliation flipped Strategy from
// "local-only" to "dynamic", the router still routed short utterances to the
// local whisper-server because PreferLocalUnderSeconds defaulted to 10s. The
// user-visible effect was "primary=HF is set but dictation still hits local
// whisper" — and whisper-server kept burning ~600MB RAM in the background.
// syncConfiguredSTTRouter must zero PreferLocalUnderSeconds alongside the
// Strategy reset when the picker moves off a local-builtin primary.
func TestSyncConfiguredSTTRouterZeroesPreferLocalUnderSecondsForCloudPrimary(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.ModelSelection.Dictate.PrimaryProfileID = "stt.huggingface.whisper-large-v3"
	cfg.Routing.Strategy = "dynamic"
	cfg.Routing.PreferLocalUnderSeconds = 10.0
	cfg.HuggingFace.Enabled = true
	cfg.HuggingFace.Model = "openai/whisper-large-v3"

	r := &router.Router{Strategy: router.StrategyDynamic, PreferLocalUnderSecs: 10.0}
	syncConfiguredSTTRouter(context.Background(), cfg, nil, r)

	if cfg.Routing.PreferLocalUnderSeconds != 0 {
		t.Fatalf("cfg.Routing.PreferLocalUnderSeconds = %v, want 0 for cloud primary", cfg.Routing.PreferLocalUnderSeconds)
	}
	if r.PreferLocalUnderSecs != 0 {
		t.Fatalf("router.PreferLocalUnderSecs = %v, want 0", r.PreferLocalUnderSecs)
	}
}

func TestSyncConfiguredSTTRouterPreservesPreferLocalUnderSecondsForLocalPrimary(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.ModelSelection.Dictate.PrimaryProfileID = "stt.local.whispercpp"
	cfg.Routing.Strategy = "local-only"
	cfg.Routing.PreferLocalUnderSeconds = 10.0

	r := &router.Router{Strategy: router.StrategyLocalOnly, PreferLocalUnderSecs: 10.0}
	syncConfiguredSTTRouter(context.Background(), cfg, nil, r)

	if cfg.Routing.PreferLocalUnderSeconds != 10.0 {
		t.Fatalf("cfg.Routing.PreferLocalUnderSeconds = %v, want 10.0 (local primary should keep prefer-local window)", cfg.Routing.PreferLocalUnderSeconds)
	}
}

func TestSyncConfiguredSTTRouterPreservesExplicitCloudOnlyStrategy(t *testing.T) {
	cfg := defaultTestConfig()
	cfg.ModelSelection.Dictate.PrimaryProfileID = "stt.huggingface.whisper-large-v3"
	cfg.Routing.Strategy = "cloud-only" // user explicitly chose cloud-only
	cfg.HuggingFace.Enabled = true
	cfg.HuggingFace.Model = "openai/whisper-large-v3"

	r := &router.Router{Strategy: router.StrategyCloudOnly}
	syncConfiguredSTTRouter(context.Background(), cfg, nil, r)

	if cfg.Routing.Strategy != "cloud-only" {
		t.Fatalf("cfg.Routing.Strategy = %q, want %q (explicit user choice must be preserved)", cfg.Routing.Strategy, "cloud-only")
	}
	if r.Strategy != router.StrategyCloudOnly {
		t.Fatalf("router.Strategy = %q, want %q", r.Strategy, router.StrategyCloudOnly)
	}
}
