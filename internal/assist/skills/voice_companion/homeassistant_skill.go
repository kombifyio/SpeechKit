package voice_companion

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

const maxHomeAssistantResponseBytes = 64 << 10

// HomeAssistantSkill delegates smart-home semantics to a local Home Assistant
// Conversation API. A matched smart-home request is terminal at this boundary:
// missing configuration, no-match responses, and failures never fall through to
// a general LLM, MCP, Gateway, or cloud path.
type HomeAssistantSkill struct {
	client          *http.Client
	probeURL        string
	conversationURL string
	token           string
	configErr       error
}

type homeAssistantCallError struct {
	reason string
}

func (e *homeAssistantCallError) Error() string {
	if e == nil || e.reason == "" {
		return "home_assistant: request failed"
	}
	return "home_assistant: request failed: " + e.reason
}

type homeAssistantConversationResult struct {
	Speech       string
	ResponseType string
	ErrorCode    string
}

// NewHomeAssistantSkill constructs the fail-closed Home Assistant boundary.
// The bearer credential may reach only a loopback or private/local endpoint.
// Plain HTTP is accepted only for literal loopback/localhost development
// endpoints; private addresses and DNS names require HTTPS.
func NewHomeAssistantSkill(baseURL, token string) *HomeAssistantSkill {
	skill := &HomeAssistantSkill{token: strings.TrimSpace(token)}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		skill.configErr = errors.New("home_assistant: URL is empty")
		return skill
	}
	if skill.token == "" {
		skill.configErr = errors.New("home_assistant: token is empty")
		return skill
	}

	validation := homeAssistantURLValidation()
	probeURL, err := netsec.BuildEndpoint(baseURL, "api/", validation)
	if err != nil {
		skill.configErr = fmt.Errorf("home_assistant: invalid local URL: %w", err)
		return skill
	}
	conversationURL, err := netsec.BuildEndpoint(baseURL, "api/conversation/process", validation)
	if err != nil {
		skill.configErr = fmt.Errorf("home_assistant: invalid local URL: %w", err)
		return skill
	}

	client := netsec.NewSafeHTTPClient(netsec.ClientOptions{
		Timeout:        5 * time.Second,
		DialValidation: &validation,
	})
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	skill.client = client
	skill.probeURL = probeURL
	skill.conversationURL = conversationURL
	return skill
}

// Intent reports the single smart-home intent owned by Home Assistant.
func (s *HomeAssistantSkill) Intent() shortcuts.Intent { return shortcuts.IntentHomeAssistant }

// Configured reports whether the URL and token passed static validation. DNS
// targets are revalidated as local-only addresses on every dial.
func (s *HomeAssistantSkill) Configured() bool {
	return s != nil && s.configErr == nil && s.client != nil && s.probeURL != "" && s.conversationURL != "" && s.token != ""
}

// Probe verifies that the configured local Home Assistant endpoint accepts the
// token. It never returns response bodies or credentials in errors.
func (s *HomeAssistantSkill) Probe(ctx context.Context) error {
	if s == nil {
		return errors.New("home_assistant: nil skill")
	}
	if s.configErr != nil {
		return s.configErr
	}
	if !s.Configured() {
		return errors.New("home_assistant: integration is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.probeURL, http.NoBody)
	if err != nil {
		return &homeAssistantCallError{reason: "request_build_failed"}
	}
	s.authorize(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return &homeAssistantCallError{reason: "unreachable"}
	}
	defer resp.Body.Close() //nolint:errcheck // response close is not actionable
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return &homeAssistantCallError{reason: "authentication_failed"}
	default:
		return &homeAssistantCallError{reason: "unexpected_status"}
	}
}

// Execute delegates one matched smart-home request to Home Assistant. Every
// branch returns a terminal ToolResult so the Assist pipeline cannot reinterpret
// the command through a general-purpose model or remote tool surface.
func (s *HomeAssistantSkill) Execute(ctx context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	if s == nil || !s.Configured() {
		return homeAssistantFailureResult(call.Locale, "not_configured"), nil
	}
	utterance := strings.TrimSpace(call.Transcript)
	if utterance == "" {
		utterance = strings.TrimSpace(call.Payload)
	}
	if utterance == "" {
		return homeAssistantFailureResult(call.Locale, "empty_command"), nil
	}

	result, err := s.callConversation(ctx, utterance, call.Locale)
	if err != nil {
		reason := "unavailable"
		var callErr *homeAssistantCallError
		if errors.As(err, &callErr) && callErr.reason != "" {
			reason = callErr.reason
		}
		return homeAssistantFailureResult(call.Locale, reason), nil //nolint:nilerr // A terminal local result prevents smart-home fallback to an LLM.
	}

	switch result.ResponseType {
	case "action_done":
		if result.Speech == "" {
			return homeAssistantFailureResult(call.Locale, "empty_response"), nil
		}
		return homeAssistantTerminalResult(result.Speech, call.Locale, "execute", assist.ResultKindUtilityAction), nil
	case "query_answer":
		if result.Speech == "" {
			return homeAssistantFailureResult(call.Locale, "empty_response"), nil
		}
		return homeAssistantTerminalResult(result.Speech, call.Locale, "respond", assist.ResultKindAnswer), nil
	case "error":
		if result.Speech != "" {
			return homeAssistantTerminalResult(result.Speech, call.Locale, "respond", assist.ResultKindUtilityAction), nil
		}
		if result.ErrorCode == "no_intent_match" || result.ErrorCode == "no_valid_targets" {
			return homeAssistantFailureResult(call.Locale, "not_matched"), nil
		}
		return homeAssistantFailureResult(call.Locale, "rejected"), nil
	default:
		return homeAssistantFailureResult(call.Locale, "invalid_response"), nil
	}
}

func (s *HomeAssistantSkill) callConversation(ctx context.Context, text, locale string) (homeAssistantConversationResult, error) {
	body, err := json.Marshal(map[string]string{
		"text":     text,
		"language": haLanguage(locale),
	})
	if err != nil {
		return homeAssistantConversationResult{}, &homeAssistantCallError{reason: "request_encode_failed"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.conversationURL, bytes.NewReader(body))
	if err != nil {
		return homeAssistantConversationResult{}, &homeAssistantCallError{reason: "request_build_failed"}
	}
	s.authorize(req)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return homeAssistantConversationResult{}, &homeAssistantCallError{reason: "unreachable"}
	}
	defer resp.Body.Close() //nolint:errcheck // response close is not actionable

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		// Parse the bounded documented response below.
	case http.StatusUnauthorized, http.StatusForbidden:
		return homeAssistantConversationResult{}, &homeAssistantCallError{reason: "authentication_failed"}
	default:
		return homeAssistantConversationResult{}, &homeAssistantCallError{reason: "unexpected_status"}
	}

	raw, err := readHomeAssistantResponse(resp.Body)
	if err != nil {
		return homeAssistantConversationResult{}, &homeAssistantCallError{reason: "invalid_response"}
	}
	var payload struct {
		Response struct {
			Speech struct {
				Plain struct {
					Speech string `json:"speech"`
				} `json:"plain"`
			} `json:"speech"`
			ResponseType string `json:"response_type"`
			Data         struct {
				Code string `json:"code"`
			} `json:"data"`
		} `json:"response"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return homeAssistantConversationResult{}, &homeAssistantCallError{reason: "invalid_response"}
	}
	speech, err := validatedHomeAssistantSpeech(payload.Response.Speech.Plain.Speech)
	if err != nil {
		return homeAssistantConversationResult{}, &homeAssistantCallError{reason: "invalid_response"}
	}
	return homeAssistantConversationResult{
		Speech:       speech,
		ResponseType: strings.TrimSpace(payload.Response.ResponseType),
		ErrorCode:    strings.TrimSpace(payload.Response.Data.Code),
	}, nil
}

func (s *HomeAssistantSkill) authorize(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Accept", "application/json")
}

func homeAssistantURLValidation() netsec.ValidationOptions {
	return netsec.ValidationOptions{
		AllowLoopback: true,
		AllowPrivate:  true,
		RequireLocal:  true,
	}
}

func readHomeAssistantResponse(body io.Reader) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(body, maxHomeAssistantResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxHomeAssistantResponseBytes {
		return nil, errors.New("home assistant response exceeds the size limit")
	}
	return raw, nil
}

func validatedHomeAssistantSpeech(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !utf8.ValidString(value) || len(value) > 4096 {
		return "", errors.New("invalid Home Assistant speech")
	}
	for _, r := range value {
		if r < 0x20 && r != '\n' && r != '\r' && r != '\t' {
			return "", errors.New("invalid Home Assistant speech")
		}
	}
	return value, nil
}

func homeAssistantTerminalResult(text, locale, action string, kind assist.ResultKind) assist.ToolResult {
	return assist.ToolResult{
		Text:      text,
		SpeakText: text,
		Action:    action,
		Locale:    locale,
		Surface:   assist.ResultSurfaceActionAck,
		Kind:      kind,
	}
}

func homeAssistantFailureResult(locale, reason string) assist.ToolResult {
	messageID := homeAssistantFailureMessageID(reason)
	message := localization.Resolve(locale, messageID)
	result := homeAssistantTerminalResult(message.Text, message.Locale, "respond", assist.ResultKindUtilityAction)
	result.MessageID = messageID
	result.ReasonCode = reason
	return result
}

func homeAssistantFailureMessageID(reason string) localization.MessageID {
	switch reason {
	case "not_configured":
		return localization.CompanionHomeAssistantNotConfigured
	case "empty_command":
		return localization.CompanionHomeAssistantCommandEmpty
	case "not_matched":
		return localization.CompanionHomeAssistantNotMatched
	case "rejected":
		return localization.CompanionHomeAssistantRejected
	default:
		return localization.CompanionHomeAssistantUnavailable
	}
}

// haLanguage maps the SpeechKit locale to the Home Assistant language code.
func haLanguage(locale string) string {
	return localization.ResolveLocale(locale)
}
