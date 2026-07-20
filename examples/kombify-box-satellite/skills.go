//go:build windows && cgo

package main

import (
	"context"
	"fmt"
	"strings"

	speechkit "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
	companionskills "github.com/kombifyio/SpeechKit/pkg/speechkit/assist/skills"
)

const (
	intentCompanionHelp   = "companion.help"
	intentCompanionStatus = "companion.status"
)

// companionSkillRouter answers box-specific "help"/"status" locally, delegates
// the deterministic framework skills (time, date, math, weather, timer,
// reminder, wikipedia, temperature) to the public pkg/speechkit/assist/skills
// catalog. Assist smart-home commands use the catalog's hardened Home Assistant
// boundary; the example-local bridge remains only for the voice-agent tool.
// Timer and Reminder actually fire through the catalog's built-in scheduler;
// onAlarm rings the box.
type companionSkillRouter struct {
	cfg     *Config
	ha      *haBridge
	catalog *companionskills.Catalog
}

func newCompanionSkillRouter(cfg *Config, onAlarm func(companionskills.Alarm)) *companionSkillRouter {
	return &companionSkillRouter{
		cfg: cfg,
		ha:  newHABridge(cfg),
		catalog: companionskills.New(companionskills.Options{
			HomeAssistantURL:   cfg.HomeAssistant.BaseURL,
			HomeAssistantToken: cfg.haToken(),
			OnAlarm:            onAlarm,
		}),
	}
}

// Close cancels any pending Timer/Reminder alarms scheduled by the catalog.
func (r *companionSkillRouter) Close() {
	if r != nil && r.catalog != nil {
		r.catalog.Close()
	}
}

func (r *companionSkillRouter) homeAssistantConfigured() bool {
	return r != nil && r.ha != nil && r.ha.configured()
}

func (r *companionSkillRouter) MatchTool(ctx context.Context, req speechkit.AssistRequest) (assist.ToolCall, bool, error) {
	if text := normalizeSkillText(req.Text); text != "" {
		if intent, ok := matchLocalCompanionIntent(text); ok {
			return assist.ToolCall{Intent: intent, Payload: req.Text, Locale: req.Locale}, true, nil
		}
	}
	// Real framework skills include the terminal Home Assistant boundary. Only
	// utterances outside all deterministic skills may continue to the LLM.
	if call, ok, err := r.catalog.Matcher().MatchTool(ctx, req); err != nil || ok {
		return call, ok, err
	}
	return assist.ToolCall{}, false, nil
}

func (r *companionSkillRouter) ExecuteTool(ctx context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	switch call.Intent {
	case intentCompanionHelp:
		return localSkillResult("help", "Ich kann Uhrzeit, Datum, Wetter, Rechenaufgaben, Timer, Erinnerungen, Wikipedia und Temperatur-Umrechnungen. Home Assistant steuere ich, sobald er verbunden ist; offene Fragen gehen ans LLM.", call.Locale), nil
	case intentCompanionStatus:
		return localSkillResult("status", r.statusText(), call.Locale), nil
	default:
		// Everything the catalog matched, including Home Assistant.
		return r.catalog.Executor().ExecuteTool(ctx, call)
	}
}

func (r *companionSkillRouter) statusText() string {
	llm := "nicht verbunden"
	if r != nil && r.cfg != nil {
		if r.cfg.localAssistProvider() && r.cfg.assistReady() {
			llm = "lokal konfiguriert"
		} else if r.cfg.assistReady() {
			llm = "bereit"
		}
	}
	ttsState := "nicht verbunden"
	if r != nil && r.cfg != nil && r.cfg.ttsReady() {
		ttsState = "bereit"
	}
	ha := "nicht verbunden"
	if r != nil && r.homeAssistantConfigured() {
		ha = "bereit"
	}
	return fmt.Sprintf("Hands-free Assist ist aktiv. Framework-Skills laufen. LLM: %s. TTS: %s. Home Assistant: %s.", llm, ttsState, ha)
}

func localSkillResult(action, text, locale string) assist.ToolResult {
	return assist.ToolResult{
		Text:      text,
		SpeakText: text,
		Action:    action,
		Kind:      "companion_skill",
		Surface:   speechkit.AssistSurfaceActionAck,
		Locale:    locale,
	}
}

func matchLocalCompanionIntent(text string) (string, bool) {
	switch {
	case hasSkillPhrase(text, "hilfe", "was kannst du", "skills", "funktionen", "was kann ich sagen"):
		return intentCompanionHelp, true
	case hasSkillPhrase(text, "status", "bist du da", "bist du bereit", "online", "test"):
		return intentCompanionStatus, true
	default:
		return "", false
	}
}

func normalizeSkillText(text string) string {
	replacer := strings.NewReplacer(
		"ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss",
		"Ä", "ae", "Ö", "oe", "Ü", "ue",
		".", " ", ",", " ", "?", " ", "!", " ", ":", " ", ";", " ",
		"\t", " ", "\n", " ",
	)
	return strings.Join(strings.Fields(strings.ToLower(replacer.Replace(text))), " ")
}

func hasSkillPhrase(text string, phrases ...string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}
