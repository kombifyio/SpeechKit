//go:build linux

package core

import (
	"context"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
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

func TestBuildAssistPipelineReturnsClientExecutableToolActions(t *testing.T) {
	t.Parallel()

	cfg, err := config.Load("/nonexistent/speechkit-test-config.toml")
	if err != nil {
		t.Fatalf("load default config: %v", err)
	}
	app := &App{Cfg: cfg, Health: NewHealthRegistry()}
	pipeline, _ := buildAssistPipeline(context.Background(), cfg, app)

	for _, tt := range []struct {
		name       string
		transcript string
		shortcut   string
		surface    assist.ResultSurface
	}{
		{
			name:       "copy last",
			transcript: "copy last",
			shortcut:   "copy_last",
			surface:    assist.ResultSurfaceActionAck,
		},
		{
			name:       "insert last",
			transcript: "insert last",
			shortcut:   "insert_last",
			surface:    assist.ResultSurfaceActionAck,
		},
		{
			name:       "summarize",
			transcript: "summarize this",
			shortcut:   "summarize",
			surface:    assist.ResultSurfacePanel,
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			result, err := pipeline.Process(context.Background(), tt.transcript, assist.ProcessOpts{Locale: "en"})
			if err != nil {
				t.Fatalf("Process(%q) error = %v", tt.transcript, err)
			}
			if got := result.Action; got != "execute" {
				t.Fatalf("action = %q, want execute", got)
			}
			if got := result.Shortcut; got != tt.shortcut {
				t.Fatalf("shortcut = %q, want %q", got, tt.shortcut)
			}
			if got := result.Surface; got != tt.surface {
				t.Fatalf("surface = %q, want %q", got, tt.surface)
			}
		})
	}
}

// TestEnableHomeAssistantUtility flips the registry entry from
// Enabled=false (default) to Enabled=true so the Assist router can
// dispatch HA-prefix matches. Regression guard for the v0.37.0 bug
// where the HA skill was registered with the CompositeExecutor but
// the resolver's intent-routing skipped the disabled UtilityDefinition.
func TestEnableHomeAssistantUtility(t *testing.T) {
	t.Parallel()

	registry := assist.DefaultUtilityRegistry()
	if _, ok := registry.Definition(shortcuts.IntentHomeAssistant); ok {
		t.Fatal("expected default registry to expose HomeAssistant as disabled (Definition returns ok=false)")
	}

	enableHomeAssistantUtility(registry)

	def, ok := registry.Definition(shortcuts.IntentHomeAssistant)
	if !ok {
		t.Fatal("expected HomeAssistant to be enabled after override")
	}
	if def.ID != assist.UtilityHomeAssistant {
		t.Errorf("UtilityDefinition.ID = %q, want %q", def.ID, assist.UtilityHomeAssistant)
	}
	if !def.Enabled {
		t.Errorf("UtilityDefinition.Enabled = false, want true")
	}
	if def.Input != assist.UtilityInputUtterance {
		t.Errorf("UtilityDefinition.Input = %q, want %q", def.Input, assist.UtilityInputUtterance)
	}
}

func TestEnableHomeAssistantUtility_NilRegistryIsSafe(t *testing.T) {
	t.Parallel()
	// Must not panic.
	enableHomeAssistantUtility(nil)
}
