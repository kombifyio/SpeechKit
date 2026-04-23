package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/internal/textactions"
)

type voiceAgentDialogTurn struct {
	Role      string
	Text      string
	CreatedAt time.Time
}

func (s *appState) resetVoiceAgentSessionSummary() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.voiceAgentDialogTurns = nil
	s.voiceAgentSessionStarted = time.Now().UTC()
	s.voiceAgentSummaryDone = false
	s.mu.Unlock()
}

func (s *appState) recordVoiceAgentDialogTurn(role, text string, done bool) {
	if s == nil || !done {
		return
	}
	role = normalizeVoiceAgentDialogRole(role)
	text = strings.TrimSpace(text)
	if role == "" || text == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.voiceAgentDialogTurns) > 0 {
		last := s.voiceAgentDialogTurns[len(s.voiceAgentDialogTurns)-1]
		if last.Role == role && last.Text == text {
			return
		}
	}
	s.voiceAgentDialogTurns = append(s.voiceAgentDialogTurns, voiceAgentDialogTurn{
		Role:      role,
		Text:      text,
		CreatedAt: time.Now().UTC(),
	})
	if len(s.voiceAgentDialogTurns) > 80 {
		s.voiceAgentDialogTurns = s.voiceAgentDialogTurns[len(s.voiceAgentDialogTurns)-80:]
	}
}

func (s *appState) voiceAgentSessionTranscript() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	turns := append([]voiceAgentDialogTurn(nil), s.voiceAgentDialogTurns...)
	s.mu.Unlock()
	return formatVoiceAgentDialogTurns(turns)
}

func (s *appState) finishVoiceAgentSessionSummary(ctx context.Context, cfg *config.Config) string {
	if s == nil || !voiceAgentSessionSummaryEnabled(cfg) {
		return ""
	}

	turns := s.voiceAgentDialogTurnSnapshot()
	transcript := formatVoiceAgentDialogTurns(turns)
	if strings.TrimSpace(transcript) == "" {
		return ""
	}
	if !s.claimVoiceAgentSessionSummaryFinalization() {
		return ""
	}

	locale := "en"
	if cfg != nil && strings.TrimSpace(cfg.General.Language) != "" {
		locale = strings.TrimSpace(cfg.General.Language)
	}

	tool := s.voiceAgentSummaryTool
	if tool.Summarizer == nil && s.summarizeFlow != nil {
		tool = textactions.SummaryTool{
			Summarizer: &textactions.FlowSummarizer{Flow: s.summarizeFlow},
		}
	}

	summary := ""
	if tool.Summarizer != nil {
		generated, err := tool.Run(ctx, textactions.Input{
			Text:        transcript,
			Instruction: voiceAgentSummaryInstruction(locale),
			Locale:      locale,
			Source:      textactions.SourceUtterance,
		})
		if err != nil && !errors.Is(err, textactions.ErrSummarizerNotConfigured) && !errors.Is(err, textactions.ErrSummarizeInputUnavailable) {
			slog.Warn("voice agent session summary failed", "err", err)
		}
		summary = strings.TrimSpace(generated)
	}
	if summary == "" {
		summary = fallbackVoiceAgentSessionSummary(transcript)
	}
	if summary == "" {
		return ""
	}

	s.sendPrompterMessage("system", fmt.Sprintf("Session summary\n%s", summary), true)
	s.persistVoiceAgentSessionSummary(ctx, cfg, turns, transcript, summary)
	s.addLog("Voice Agent session summary created", "success")
	return summary
}

func (s *appState) claimVoiceAgentSessionSummaryFinalization() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.voiceAgentSummaryDone {
		return false
	}
	s.voiceAgentSummaryDone = true
	return true
}

func (s *appState) voiceAgentDialogTurnSnapshot() []voiceAgentDialogTurn {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	turns := append([]voiceAgentDialogTurn(nil), s.voiceAgentDialogTurns...)
	s.mu.Unlock()
	return turns
}

func (s *appState) persistVoiceAgentSessionSummary(ctx context.Context, cfg *config.Config, turns []voiceAgentDialogTurn, transcript, summary string) {
	if s == nil || s.voiceAgentStore == nil {
		return
	}

	s.mu.Lock()
	startedAt := s.voiceAgentSessionStarted
	activeProfiles := cloneStringMap(s.activeProfiles)
	s.mu.Unlock()
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}

	language := "en"
	if cfg != nil && strings.TrimSpace(cfg.General.Language) != "" {
		language = strings.TrimSpace(cfg.General.Language)
	}
	runtimeKind := "native_realtime"
	if cfg != nil && cfg.VoiceAgent.PipelineFallback {
		runtimeKind = "pipeline_fallback"
	}
	profileID := activeProfiles["realtime_voice"]
	if profileID == "" && cfg != nil {
		profileID = cfg.ModelSelection.VoiceAgent.PrimaryProfileID
	}

	storeTurns := make([]store.VoiceAgentTurn, 0, len(turns))
	for _, turn := range turns {
		storeTurns = append(storeTurns, store.VoiceAgentTurn{
			Role:      turn.Role,
			Text:      turn.Text,
			CreatedAt: turn.CreatedAt,
		})
	}
	_, err := s.voiceAgentStore.SaveVoiceAgentSession(ctx, store.VoiceAgentSession{
		StartedAt:         startedAt,
		EndedAt:           time.Now().UTC(),
		Language:          language,
		ProviderProfileID: profileID,
		RuntimeKind:       runtimeKind,
		Transcript:        transcript,
		Turns:             storeTurns,
		Summary: store.VoiceAgentSessionSummary{
			Title:   voiceAgentSummaryTitle(summary),
			Summary: summary,
			RawText: summary,
		},
	})
	if err != nil {
		slog.Warn("voice agent session summary persistence failed", "err", err)
	}
}

func voiceAgentSessionSummaryEnabled(cfg *config.Config) bool {
	if cfg == nil {
		return true
	}
	return cfg.VoiceAgent.EnableSessionSummary
}

func normalizeVoiceAgentDialogRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user":
		return "user"
	case "assistant":
		return "assistant"
	default:
		return ""
	}
}

func formatVoiceAgentDialogTurns(turns []voiceAgentDialogTurn) string {
	var builder strings.Builder
	for _, turn := range turns {
		role := normalizeVoiceAgentDialogRole(turn.Role)
		text := strings.TrimSpace(turn.Text)
		if role == "" || text == "" {
			continue
		}
		if builder.Len() > 0 {
			builder.WriteString("\n")
		}
		if role == "user" {
			builder.WriteString("User: ")
		} else {
			builder.WriteString("Assistant: ")
		}
		builder.WriteString(strings.Join(strings.Fields(text), " "))
	}
	return builder.String()
}

func voiceAgentSummaryTitle(summary string) string {
	words := strings.Fields(summary)
	if len(words) == 0 {
		return "Voice Agent session"
	}
	if len(words) > 8 {
		words = words[:8]
	}
	return strings.Join(words, " ")
}

func voiceAgentSummaryInstruction(locale string) string {
	switch strings.ToLower(strings.TrimSpace(locale)) {
	case "de", "de-de":
		return "Erstelle eine knappe Dialogzusammenfassung mit Ergebnissen, Entscheidungen, offenen Punkten und naechsten Schritten, sofern vorhanden."
	default:
		return "Create a concise dialog summary with outcomes, decisions, open points, and next steps where present."
	}
}

func fallbackVoiceAgentSessionSummary(transcript string) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(transcript)), " ")
	if normalized == "" {
		return ""
	}
	const maxLen = 700
	if len(normalized) <= maxLen {
		return normalized
	}
	return strings.TrimSpace(normalized[:maxLen]) + "..."
}
