//go:build linux

package core

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist/shortcuts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist/skills"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
)

func TestLocalLLMHealthURL_StripsOpenAIPath(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"http://speechkit-llm:8080/v1":  "http://speechkit-llm:8080/health",
		"http://speechkit-llm:8080/v1/": "http://speechkit-llm:8080/health",
		"http://speechkit-llm:8080":     "http://speechkit-llm:8080/health",
	}

	for input, want := range tests {
		input, want := input, want
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			if got := localLLMHealthURL(input); got != want {
				t.Fatalf("localLLMHealthURL(%q) = %q, want %q", input, got, want)
			}
		})
	}
}

func newTestAssistService(t *testing.T) *assistService {
	t.Helper()
	cfg, err := config.Load("/nonexistent/speechkit-test-config.toml")
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	app := &App{Cfg: cfg, Health: NewHealthRegistry()}
	service, _, err := buildAssistService(context.Background(), cfg, app)
	if err != nil {
		t.Fatalf("buildAssistService: %v", err)
	}
	return service
}

func TestBuildAssistServiceReturnsClientExecutableToolActions(t *testing.T) {
	t.Parallel()

	service := newTestAssistService(t)

	for _, tt := range []struct {
		name       string
		transcript string
		shortcut   string
		surface    speechkit.AssistSurfaceDecision
		kind       string
	}{
		{
			name:       "copy last",
			transcript: "copy last",
			shortcut:   "copy_last",
			surface:    speechkit.AssistSurfaceActionAck,
			kind:       "utility_action",
		},
		{
			name:       "insert last",
			transcript: "insert last",
			shortcut:   "insert_last",
			surface:    speechkit.AssistSurfaceActionAck,
			kind:       "utility_action",
		},
		{
			name:       "summarize",
			transcript: "summarize this",
			shortcut:   "summarize",
			surface:    speechkit.AssistSurfacePanel,
			kind:       "work_product",
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.Process(context.Background(), speechkit.AssistRequest{Text: tt.transcript, Locale: "en"})
			if err != nil {
				t.Fatalf("Process(%q) error = %v", tt.transcript, err)
			}
			if got := result.Action; got != "execute" {
				t.Fatalf("action = %q, want execute", got)
			}
			if got := result.ShortcutID; got != tt.shortcut {
				t.Fatalf("shortcut = %q, want %q", got, tt.shortcut)
			}
			if got := result.Surface; got != tt.surface {
				t.Fatalf("surface = %q, want %q", got, tt.surface)
			}
			if got := result.Kind; got != tt.kind {
				t.Fatalf("kind = %q, want %q from the utility registry defaults", got, tt.kind)
			}
			if result.SpeakText != result.Text || result.Text == "" {
				t.Fatalf("result = %#v, want the client-executable acknowledgement text spoken as-is", result)
			}
		})
	}
}

// A recognised smart-home command with no configured Home Assistant bridge
// is a localized terminal denial on the server too — it never becomes an
// Assist model prompt (regression guard for the v0.37.0 routing bug where a
// disabled utility let the intent through).
func TestBuildAssistServiceHomeAssistantFailsClosedWhenUnconfigured(t *testing.T) {
	t.Parallel()

	service := newTestAssistService(t)
	result, err := service.Process(context.Background(), speechkit.AssistRequest{Text: "turn on the kitchen light", Locale: "en"})
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if result.ShortcutID != string(shortcuts.IntentHomeAssistant) || result.ReasonCode != "not_configured" {
		t.Fatalf("result = %#v, want the fail-closed Home Assistant denial", result)
	}
	if result.MessageID != localization.CompanionHomeAssistantNotConfigured || result.Text == "" || result.Action == "silent" {
		t.Fatalf("result = %#v, want a localized terminal denial", result)
	}
}

func TestBuildAssistUtilityRegistryAppliesAllowList(t *testing.T) {
	t.Parallel()

	full := buildAssistUtilityRegistry(&config.Config{})
	if !full.Supports(shortcuts.IntentInsertLast) || !full.Supports(shortcuts.IntentTime) {
		t.Fatal("no allow-list must expose the default registry as-is")
	}
	if full.Supports(shortcuts.IntentHomeAssistant) {
		t.Fatal("the registry handed to /settings must keep Home Assistant opt-in")
	}

	cfg := &config.Config{}
	cfg.Assist.EnabledTools = []string{"copy_last", " time ", ""}
	filtered := buildAssistUtilityRegistry(cfg)
	if !filtered.Supports(shortcuts.IntentCopyLast) || !filtered.Supports(shortcuts.IntentTime) {
		t.Fatalf("allow-listed utilities missing: %+v", filtered.List())
	}
	if filtered.Supports(shortcuts.IntentInsertLast) || filtered.Supports(shortcuts.IntentSummarize) {
		t.Fatalf("utilities outside the allow-list must be dropped: %+v", filtered.List())
	}

	// The catalog still claims the smart-home intent regardless of the
	// allow-list, so recognised commands fail closed instead of reaching the
	// model.
	catalog := skills.New(skills.Options{Registry: filtered})
	if !catalog.Registry().Supports(shortcuts.IntentHomeAssistant) {
		t.Fatal("catalog must always claim the Home Assistant intent")
	}
}
