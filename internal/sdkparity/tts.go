package sdkparity

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type TTSProviderKind string

const (
	TTSProviderKindLocalBuiltIn   TTSProviderKind = "local_built_in"
	TTSProviderKindLocalProvider  TTSProviderKind = "local_provider"
	TTSProviderKindCloudProvider  TTSProviderKind = "cloud_provider"
	TTSProviderKindDirectProvider TTSProviderKind = "direct_provider"
)

type TTSStrategy string

const (
	TTSStrategyCloudFirst TTSStrategy = "cloud-first"
	TTSStrategyCloudOnly  TTSStrategy = "cloud-only"
	TTSStrategyLocalOnly  TTSStrategy = "local-only"
)

type TTSProviderSpec struct {
	Name string
	Kind TTSProviderKind
	Err  error
}

type TTSRouterResult struct {
	Provider string
	Calls    []string
}

type TTSRouterHarness struct {
	Synthesize func(ctx context.Context, strategy TTSStrategy, providers []TTSProviderSpec) (TTSRouterResult, error)
}

func RunTTSRouterParity(t *testing.T, h TTSRouterHarness) {
	t.Helper()
	if h.Synthesize == nil {
		t.Fatal("TTSRouterHarness.Synthesize is required")
	}

	t.Run("local_only_uses_local_built_in", func(t *testing.T) {
		result, err := h.Synthesize(context.Background(), TTSStrategyLocalOnly, []TTSProviderSpec{
			{Name: "huggingface", Kind: TTSProviderKindCloudProvider},
			{Name: "piper", Kind: TTSProviderKindLocalBuiltIn},
		})
		if err != nil {
			t.Fatalf("Synthesize: %v", err)
		}
		assertTTSResult(t, result, "piper", []string{"piper"})
	})

	t.Run("local_only_uses_local_provider", func(t *testing.T) {
		result, err := h.Synthesize(context.Background(), TTSStrategyLocalOnly, []TTSProviderSpec{
			{Name: "huggingface", Kind: TTSProviderKindCloudProvider},
			{Name: "openedai-kokoro", Kind: TTSProviderKindLocalProvider},
		})
		if err != nil {
			t.Fatalf("Synthesize: %v", err)
		}
		assertTTSResult(t, result, "openedai-kokoro", []string{"openedai-kokoro"})
	})

	t.Run("cloud_only_skips_local_provider_kinds", func(t *testing.T) {
		result, err := h.Synthesize(context.Background(), TTSStrategyCloudOnly, []TTSProviderSpec{
			{Name: "piper", Kind: TTSProviderKindLocalBuiltIn},
			{Name: "openai", Kind: TTSProviderKindDirectProvider},
		})
		if err != nil {
			t.Fatalf("Synthesize: %v", err)
		}
		assertTTSResult(t, result, "openai", []string{"openai"})
	})

	t.Run("cloud_first_falls_back_in_order", func(t *testing.T) {
		result, err := h.Synthesize(context.Background(), TTSStrategyCloudFirst, []TTSProviderSpec{
			{Name: "openai", Kind: TTSProviderKindDirectProvider, Err: errors.New("rate limited")},
			{Name: "google", Kind: TTSProviderKindDirectProvider},
		})
		if err != nil {
			t.Fatalf("Synthesize: %v", err)
		}
		assertTTSResult(t, result, "google", []string{"openai", "google"})
	})

	t.Run("local_only_with_no_local_provider_returns_no_eligible_error", func(t *testing.T) {
		result, err := h.Synthesize(context.Background(), TTSStrategyLocalOnly, []TTSProviderSpec{
			{Name: "openai", Kind: TTSProviderKindDirectProvider},
		})
		if err == nil {
			t.Fatal("expected no eligible providers error")
		}
		if !strings.Contains(err.Error(), "no eligible providers") {
			t.Fatalf("error = %q, want no eligible providers", err.Error())
		}
		assertTTSResult(t, result, "", nil)
	})
}

func assertTTSResult(t *testing.T, got TTSRouterResult, wantProvider string, wantCalls []string) {
	t.Helper()
	if got.Provider != wantProvider {
		t.Fatalf("Provider = %q, want %q", got.Provider, wantProvider)
	}
	if len(got.Calls) == 0 && len(wantCalls) == 0 {
		return
	}
	if !reflect.DeepEqual(got.Calls, wantCalls) {
		t.Fatalf("Calls = %v, want %v", got.Calls, wantCalls)
	}
}
