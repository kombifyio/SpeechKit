package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/config"
	"github.com/kombifyio/SpeechKit/internal/textactions"
)

type voiceAgentDialogTurn struct {
	Role string
	Text string
}

func (s *appState) resetVoiceAgentSessionSummary() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.voiceAgentDialogTurns = nil
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
		Role: role,
		Text: text,
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

	transcript := s.voiceAgentSessionTranscript()
	if strings.TrimSpace(transcript) == "" {
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
	s.addLog("Voice Agent session summary created", "success")
	return summary
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
