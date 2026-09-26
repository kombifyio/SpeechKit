package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestVoiceAgentSessionsSaveAndList(t *testing.T) {
	s, err := NewSQLiteStore(StoreConfig{
		SQLitePath: filepath.Join(t.TempDir(), "feedback.db"),
		SaveAudio:  false,
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer s.Close()

	sessionStore, ok := any(s).(VoiceAgentSessionStore)
	if !ok {
		t.Fatal("sqlite store does not implement VoiceAgentSessionStore")
	}

	startedAt := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	endedAt := startedAt.Add(90 * time.Second)
	id, err := sessionStore.SaveVoiceAgentSession(context.Background(), VoiceAgentSession{
		StartedAt:         startedAt,
		EndedAt:           endedAt,
		Language:          "de",
		ProviderProfileID: "realtime.openai.gpt-realtime-2",
		RuntimeKind:       "native_realtime",
		Transcript:        "User: Idee\nAssistant: Naechster Schritt",
		Turns: []VoiceAgentTurn{
			{Role: "user", Text: "Idee", CreatedAt: startedAt},
			{Role: "assistant", Text: "Naechster Schritt", CreatedAt: endedAt},
		},
		Summary: VoiceAgentSessionSummary{
			Summary:       "Plan fuer den naechsten Schritt.",
			Ideas:         []string{"Produktreife"},
			Decisions:     []string{"Live UX zuerst"},
			OpenQuestions: []string{"Signing"},
			NextSteps:     []string{"Build pruefen"},
		},
	})
	if err != nil {
		t.Fatalf("SaveVoiceAgentSession: %v", err)
	}
	if id == 0 {
		t.Fatal("SaveVoiceAgentSession id = 0")
	}

	sessions, err := sessionStore.ListVoiceAgentSessions(context.Background(), ListOpts{Limit: 10})
	if err != nil {
		t.Fatalf("ListVoiceAgentSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	got := sessions[0]
	if got.Summary.Summary != "Plan fuer den naechsten Schritt." {
		t.Fatalf("summary = %q", got.Summary.Summary)
	}
	if got.Summary.Title == "" {
		t.Fatal("summary title should be derived")
	}
	if got.Transcript != "" || len(got.Turns) != 0 || got.Summary.RawText != "" {
		t.Fatalf("list response should be light, got %#v", got)
	}
	detail, err := sessionStore.GetVoiceAgentSession(context.Background(), id)
	if err != nil {
		t.Fatalf("GetVoiceAgentSession: %v", err)
	}
	if len(detail.Turns) != 2 || detail.Turns[0].Role != "user" {
		t.Fatalf("detail turns = %#v", detail.Turns)
	}
	if len(detail.Summary.NextSteps) != 1 || detail.Summary.NextSteps[0] != "Build pruefen" {
		t.Fatalf("detail next steps = %#v", detail.Summary.NextSteps)
	}
	filtered, err := sessionStore.ListVoiceAgentSessions(context.Background(), ListOpts{Limit: 10, Language: "de-DE", After: startedAt})
	if err != nil {
		t.Fatalf("ListVoiceAgentSessions filtered: %v", err)
	}
	if len(filtered) != 1 || filtered[0].ID != id {
		t.Fatalf("filtered voice sessions = %+v, want saved session", filtered)
	}

	var turnCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM voice_agent_session_turns WHERE session_id = ?`, id).Scan(&turnCount); err != nil {
		t.Fatalf("query normalized turns: %v", err)
	}
	if turnCount != 2 {
		t.Fatalf("normalized turn rows = %d, want 2", turnCount)
	}
	var itemCount int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM voice_agent_session_summary_items WHERE session_id = ?`, id).Scan(&itemCount); err != nil {
		t.Fatalf("query normalized summary items: %v", err)
	}
	if itemCount != 4 {
		t.Fatalf("normalized summary item rows = %d, want 4", itemCount)
	}
}
