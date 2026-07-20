//go:build windows && cgo

package main

// The Box Home Assistant adapter reuses the public Voice-Companion catalog's
// hardened Home Assistant boundary for realtime-agent function calls. A named
// home_assistant tool invocation is already a smart-home classification, so the
// adapter always claims it and always returns a terminal result. Missing
// configuration, Home Assistant no-match responses, and failures must never
// hand the query back to the realtime model for reinterpretation.

import (
	"context"
	"fmt"
	"log"
	"strings"

	speechkit "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/agentkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
	companionskills "github.com/kombifyio/SpeechKit/pkg/speechkit/assist/skills"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/assist/toolbridge"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

const intentHomeAssistant = "home_assistant"

const homeAssistantAuthorityInstruction = "Home Assistant is the sole authority for commands recognized by the host's deterministic smart-home command policy. You MUST call the home_assistant tool for every such request. Never answer, reinterpret, or simulate a recognized smart-home result yourself, including when the tool reports unavailable, rejected, or no matching target."

type haBridge struct {
	matcher  assist.ToolMatcher
	executor assist.ToolExecutor
	ready    bool
	language string
}

func newHABridge(cfg *Config) *haBridge {
	baseURL := ""
	token := ""
	language := ""
	if cfg != nil {
		baseURL = strings.TrimSpace(cfg.HomeAssistant.BaseURL)
		token = strings.TrimSpace(cfg.haToken())
		language = strings.TrimSpace(cfg.HomeAssistant.Language)
	}
	catalog := companionskills.New(companionskills.Options{
		HomeAssistantURL:   baseURL,
		HomeAssistantToken: token,
	})
	return &haBridge{
		matcher:  catalog.Matcher(),
		executor: catalog.Executor(),
		ready:    validLocalHomeAssistantConfig(baseURL, token),
		language: language,
	}
}

func (h *haBridge) configured() bool { return h != nil && h.ready }

// classifiesTranscript applies the same deterministic catalog matcher used by
// one-shot Assist. It performs no model or network call. Only the catalog's
// explicit Home Assistant lexicon is part of the realtime smart-home surface.
func (h *haBridge) classifiesTranscript(text, locale string) (bool, error) {
	if h == nil || h.matcher == nil {
		return false, fmt.Errorf("home_assistant: deterministic matcher is unavailable")
	}
	call, matched, err := h.matcher.MatchTool(context.Background(), speechkit.AssistRequest{
		Text:   text,
		Locale: locale,
	})
	if err != nil {
		return false, err
	}
	return matched && call.Intent == intentHomeAssistant, nil
}

// MatchTool always claims nonempty named Home Assistant tool calls. This
// matcher is not used for general Assist routing; the realtime provider has
// already selected the dedicated home_assistant function before toolbridge
// invokes it.
func (h *haBridge) MatchTool(_ context.Context, req speechkit.AssistRequest) (assist.ToolCall, bool, error) {
	return assist.ToolCall{
		Intent:  intentHomeAssistant,
		Payload: req.Text,
		Locale:  h.locale(req.Locale),
	}, true, nil
}

// ExecuteTool delegates to the hardened shared Home Assistant skill and
// converts any unexpected adapter failure into a terminal local denial.
func (h *haBridge) ExecuteTool(ctx context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	locale := h.locale(call.Locale)
	if h == nil || h.executor == nil {
		return boxHomeAssistantUnavailable(locale), nil
	}
	call.Intent = intentHomeAssistant
	call.Locale = locale
	result, err := h.executor.ExecuteTool(ctx, call)
	if err != nil || result.Surface == speechkit.AssistSurfaceSilent || strings.TrimSpace(result.Text) == "" {
		return boxHomeAssistantUnavailable(locale), nil
	}
	return result, nil
}

func (h *haBridge) locale(requestLocale string) string {
	if h != nil && h.language != "" {
		return h.language
	}
	return requestLocale
}

func validLocalHomeAssistantConfig(baseURL, token string) bool {
	if strings.TrimSpace(token) == "" {
		return false
	}
	return netsec.ValidateProviderURL(strings.TrimSpace(baseURL), netsec.ValidationOptions{
		AllowLoopback: true,
		AllowPrivate:  true,
		RequireLocal:  true,
	}) == nil
}

func boxHomeAssistantUnavailable(locale string) assist.ToolResult {
	messageID := localization.CompanionHomeAssistantUnavailable
	message := localization.Resolve(locale, messageID)
	return assist.ToolResult{
		Text:       message.Text,
		SpeakText:  message.Text,
		Action:     "respond",
		Kind:       "utility_action",
		Surface:    speechkit.AssistSurfaceActionAck,
		Locale:     message.Locale,
		MessageID:  messageID,
		ReasonCode: "unavailable",
	}
}

// registerHomeAssistantTool installs the authority boundary even when Home
// Assistant is not configured. Omitting the tool would allow the realtime
// model to answer a recognized smart-home request from its own knowledge.
func registerHomeAssistantTool(registry *agentkit.ToolRegistry, cfg *Config, sessionKey string) (*haBridge, error) {
	if registry == nil {
		return nil, fmt.Errorf("home_assistant: tool registry is nil")
	}
	ha := newHABridge(cfg)
	locale := ""
	if cfg != nil {
		locale = cfg.VoiceAgent.Locale
	}
	tool, err := toolbridge.New(toolbridge.Options{
		Name: "home_assistant",
		Description: "Required authority for commands recognized by the host's deterministic smart-home command policy. " +
			"Always call this tool for those commands; never answer or simulate their results from model knowledge.",
		Matcher:       ha,
		Executor:      ha,
		DefaultLocale: locale,
		SessionKey:    strings.TrimSpace(sessionKey),
	})
	if err != nil {
		return nil, err
	}
	if err := registry.Register(tool); err != nil {
		return nil, err
	}
	if ha.configured() && cfg != nil {
		log.Printf("[voice_agent] home_assistant authority tool ready (%s)", cfg.HomeAssistant.BaseURL)
	} else {
		log.Printf("[voice_agent] home_assistant authority tool installed fail-closed (integration unavailable)")
	}
	return ha, nil
}

func withHomeAssistantAuthority(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return homeAssistantAuthorityInstruction
	}
	return base + "\n\n" + homeAssistantAuthorityInstruction
}
