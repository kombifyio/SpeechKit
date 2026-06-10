//go:build linux

package core

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/server/persona"
	vsserver "github.com/kombifyio/SpeechKit/internal/server/voiceagent"
	"github.com/kombifyio/SpeechKit/internal/voiceagentprofile"
)

func TestPersonaResolverAppliesConfiguredAgentProfileWhenStartOmitsPersona(t *testing.T) {
	cfg := &config.Config{}
	cfg.General.Language = "de"
	cfg.VoiceAgent.AgentProfileID = voiceagentprofile.BrainstormingCompanionID
	cfg.VoiceAgent.Voice = "Kore"
	cfg.VoiceAgent.FrameworkPrompt = "Base prompt"

	reg := persona.NewRegistry()
	persona.LoadSeeds(reg, cfg)
	resolver := &personaResolver{cfg: cfg, registry: reg}

	frame, err := resolver.Resolve(vsserver.StartFrame{})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if frame.Voice != "Aoede" {
		t.Fatalf("Voice = %q, want Aoede", frame.Voice)
	}
	if !strings.Contains(frame.SystemPrompt, "blind spots") {
		t.Fatalf("SystemPrompt should contain brainstorming profile instructions, got %q", frame.SystemPrompt)
	}
}

func TestMoshiProviderFactoryIsExplicitlyExperimentalUnavailable(t *testing.T) {
	cfg := &config.Config{}
	app := &App{}

	factory, status, err := buildProviderFactory(context.Background(), cfg, app, ProviderMoshi)
	if err != nil {
		t.Fatalf("buildProviderFactory() error = %v", err)
	}
	if !strings.Contains(status, "experimental_unavailable") {
		t.Fatalf("status = %q, want experimental_unavailable marker", status)
	}

	provider := factory.NewProvider()
	if provider.Name() != "moshi-stub" {
		t.Fatalf("provider name = %q, want moshi-stub", provider.Name())
	}
	err = provider.Connect(context.Background(), vsserver.LiveConfigFrame{})
	if err == nil || !strings.Contains(err.Error(), "experimental_unavailable") {
		t.Fatalf("Connect() error = %v, want experimental_unavailable marker", err)
	}
}

func TestBuildVoiceAgentHandlerUsesConfiguredTicketTTL(t *testing.T) {
	cfg := &config.Config{}
	cfg.General.Language = "en"
	cfg.Server.TicketTTLSec = 90
	cfg.Server.MaxVoiceAgentSessions = 10
	cfg.Server.MaxSessionsPerUser = 10
	cfg.VoiceAgent.Provider = ProviderGemini
	app := &App{PersonaRegistry: persona.NewRegistry()}

	h, _, err := buildVoiceAgentHandler(context.Background(), cfg, app)
	if err != nil {
		t.Fatalf("buildVoiceAgentHandler() error = %v", err)
	}
	mux := http.NewServeMux()
	h.Mount(mux)
	handler := middleware.Auth(middleware.AuthOptions{Mode: string(middleware.AuthModeNone)})(mux)

	before := time.Now().UTC()
	req := httptest.NewRequest(http.MethodPost, "https://speechkit.test/v1/voiceagent/sessions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create session status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		ExpiresAt string `json:"expires_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	expires, err := time.Parse(time.RFC3339, body.ExpiresAt)
	if err != nil {
		t.Fatalf("parse expires_at %q: %v", body.ExpiresAt, err)
	}
	delta := expires.Sub(before)
	if delta < 89*time.Second || delta > 92*time.Second {
		t.Fatalf("expires_at delta = %s, want configured 90s TTL", delta)
	}
}

func TestBuildVoiceAgentHandlerRegistersOpenAIAsSwitchableProvider(t *testing.T) {
	cfg := &config.Config{}
	cfg.General.Language = "en"
	cfg.Server.MaxVoiceAgentSessions = 10
	cfg.Server.MaxSessionsPerUser = 10
	cfg.VoiceAgent.Provider = ProviderGemini
	app := &App{PersonaRegistry: persona.NewRegistry()}

	_, status, err := buildVoiceAgentHandler(context.Background(), cfg, app)
	if err != nil {
		t.Fatalf("buildVoiceAgentHandler() error = %v", err)
	}
	if !strings.Contains(status, "switchable:") {
		t.Fatalf("status = %q, want a switchable provider list", status)
	}
	// Native realtime providers must be per-session switchable regardless of
	// the configured default; missing keys degrade the session at upgrade,
	// they must not silently drop the provider from the switchable set.
	for _, p := range []string{ProviderDeepgram, ProviderOpenAI} {
		if !strings.Contains(status, p) {
			t.Fatalf("status = %q, want switchable provider %q registered", status, p)
		}
	}
}

func TestBuildVoiceAgentHandlerUsesVoiceAgentLimitsSection(t *testing.T) {
	cfg := &config.Config{}
	cfg.General.Language = "en"
	cfg.Server.MaxVoiceAgentSessions = 10
	cfg.Server.MaxSessionsPerUser = 10
	cfg.VoiceAgent.Limits.MaxGlobalSessions = 1
	cfg.VoiceAgent.Limits.MaxPerIdentitySessions = 1
	cfg.VoiceAgent.Provider = ProviderGemini
	app := &App{PersonaRegistry: persona.NewRegistry()}

	h, _, err := buildVoiceAgentHandler(context.Background(), cfg, app)
	if err != nil {
		t.Fatalf("buildVoiceAgentHandler() error = %v", err)
	}
	mux := http.NewServeMux()
	h.Mount(mux)
	handler := middleware.Auth(middleware.AuthOptions{Mode: string(middleware.AuthModeNone)})(mux)

	req := httptest.NewRequest(http.MethodPost, "https://speechkit.test/v1/voiceagent/sessions", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first create status = %d body=%s", rec.Code, rec.Body.String())
	}
	req = httptest.NewRequest(http.MethodPost, "https://speechkit.test/v1/voiceagent/sessions", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("second create status = %d body=%s, want global limit 503", rec.Code, rec.Body.String())
	}
}

func TestPersonaResolverDefaultProfileKeepsServerPromptAndVoice(t *testing.T) {
	cfg := &config.Config{}
	cfg.General.Language = "de"
	cfg.VoiceAgent.AgentProfileID = voiceagentprofile.DefaultID
	cfg.VoiceAgent.Voice = "Kore"
	cfg.VoiceAgent.FrameworkPrompt = "Base prompt"

	reg := persona.NewRegistry()
	persona.LoadSeeds(reg, cfg)
	resolver := &personaResolver{cfg: cfg, registry: reg}

	frame, err := resolver.Resolve(vsserver.StartFrame{})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if frame.Voice != "Kore" {
		t.Fatalf("Voice = %q, want Kore", frame.Voice)
	}
	if frame.SystemPrompt != "Base prompt" {
		t.Fatalf("SystemPrompt = %q, want Base prompt", frame.SystemPrompt)
	}
}

func TestPersonaResolverOpenAIUsesRealtimeModelDefault(t *testing.T) {
	cfg := config.Config{}
	cfg.VoiceAgent.Provider = ProviderOpenAI
	cfg.VoiceAgent.Model = "gemini-3.1-flash-live-preview"
	cfg.Providers.OpenAI.RealtimeModel = "gpt-realtime-2"

	resolver := &personaResolver{cfg: &cfg, registry: persona.NewRegistry()}

	frame, err := resolver.Resolve(vsserver.StartFrame{})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if frame.Model != "gpt-realtime-2" {
		t.Fatalf("Model = %q, want gpt-realtime-2", frame.Model)
	}
}

func TestPersonaResolverResolveStepProjectsSequenceMetadata(t *testing.T) {
	cfg := &config.Config{}
	cfg.General.Language = "en"
	cfg.VoiceAgent.Voice = "Kore"
	cfg.VoiceAgent.FrameworkPrompt = "Base prompt"

	reg := persona.NewRegistry()
	_, _ = reg.UpsertPersona(persona.Persona{ID: "host", DisplayName: "Host", DefaultRole: "moderator"})
	_, _ = reg.UpsertRole(persona.Role{ID: "moderator", DisplayName: "Moderator", SystemPrompt: "Moderate the room."})
	_, _ = reg.UpsertSequence(persona.Sequence{
		ID: "meeting", DisplayName: "Meeting", Completion: "explicit_close",
		Steps: []persona.SequenceStep{
			{ID: "open", Instruction: "Open the meeting."},
			{ID: "discuss", Instruction: "Moderate discussion.", ExitCriteria: "Discussion is complete.", MaxTurns: 3},
		},
	})
	resolver := &personaResolver{cfg: cfg, registry: reg}

	frame, err := resolver.ResolveStep(vsserver.StartFrame{PersonaID: "host", SequenceID: "meeting"}, 1)
	if err != nil {
		t.Fatalf("ResolveStep: %v", err)
	}
	if frame.SequenceID != "meeting" || frame.StepID != "discuss" || frame.StepIndex != 1 || frame.StepCount != 2 {
		t.Fatalf("workflow metadata = %+v", frame)
	}
	if frame.StepInstruction != "Moderate discussion." || frame.StepExitCriteria != "Discussion is complete." || frame.StepMaxTurns != 3 {
		t.Fatalf("step metadata = %+v", frame)
	}
	if !strings.Contains(frame.SystemPrompt, "Moderate discussion.") {
		t.Fatalf("SystemPrompt missing step instruction: %q", frame.SystemPrompt)
	}
}
