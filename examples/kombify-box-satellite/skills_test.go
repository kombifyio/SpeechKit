//go:build windows && cgo

package main

import (
	"context"
	"testing"

	speechkit "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
)

func TestCompanionSkillRouterMatchesLocalSkills(t *testing.T) {
	router := newCompanionSkillRouter(&Config{}, nil)
	defer router.Close()
	tests := []struct {
		text string
		want string
	}{
		{"was kannst du", intentCompanionHelp},
		{"bist du bereit", intentCompanionStatus},
	}
	for _, tt := range tests {
		call, matched, err := router.MatchTool(context.Background(), speechkit.AssistRequest{Text: tt.text, Locale: "de"})
		if err != nil {
			t.Fatalf("MatchTool(%q) error = %v", tt.text, err)
		}
		if !matched || call.Intent != tt.want {
			t.Fatalf("MatchTool(%q) = matched %v intent %q, want %q", tt.text, matched, call.Intent, tt.want)
		}
	}
}

func TestCompanionSkillRouterDelegatesToFrameworkCatalog(t *testing.T) {
	router := newCompanionSkillRouter(&Config{}, nil)
	defer router.Close()
	// Time is now handled by the real framework skill via the public catalog,
	// not a box-local reimplementation.
	call, matched, err := router.MatchTool(context.Background(), speechkit.AssistRequest{Text: "wie spaet ist es", Locale: "de"})
	if err != nil || !matched {
		t.Fatalf("MatchTool(time) matched=%v err=%v", matched, err)
	}
	if call.Intent != "time" {
		t.Fatalf("intent = %q, want the framework \"time\" intent", call.Intent)
	}
}

func TestCompanionSkillRouterStatusMentionsReadiness(t *testing.T) {
	router := newCompanionSkillRouter(&Config{}, nil)
	result, err := router.ExecuteTool(context.Background(), mustMatch(t, router, "status"))
	if err != nil {
		t.Fatalf("ExecuteTool(status) error = %v", err)
	}
	if result.Kind != "companion_skill" || result.SpeakText == "" {
		t.Fatalf("ExecuteTool(status) = kind %q speak %q", result.Kind, result.SpeakText)
	}
}

func TestNormalizeSkillTextHandlesGermanUmlauts(t *testing.T) {
	got := normalizeSkillText("Wie spät ist es?")
	want := "wie spaet ist es"
	if got != want {
		t.Fatalf("normalizeSkillText() = %q, want %q", got, want)
	}
}

func mustMatch(t *testing.T, router *companionSkillRouter, text string) assist.ToolCall {
	t.Helper()
	call, matched, err := router.MatchTool(context.Background(), speechkit.AssistRequest{Text: text, Locale: "de"})
	if err != nil {
		t.Fatalf("MatchTool(%q) error = %v", text, err)
	}
	if !matched {
		t.Fatalf("MatchTool(%q) did not match", text)
	}
	return call
}
