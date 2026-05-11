//go:build linux

package core

import (
	"context"
	"strings"
	"testing"

	"github.com/kombifyio/SpeechKit/internal/config"
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
