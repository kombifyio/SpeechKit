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

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

// HomeAssistantSkill bridges the SpeechKit voice-companion to a
// Home Assistant Conversation API endpoint.
//
// Marcels kombify stack ships a pre-configured HA instance, so the
// natural smart-home path for the Voice-Companion is "Hey Quby, switch
// the kitchen light off" → SpeechKit STT + intent-resolve → HA
// Conversation API call → HA executes via its own integrations →
// SpeechKit speaks back HA's textual response.
//
// HA's Conversation API
// (https://developers.home-assistant.io/docs/intent_conversation_api)
// accepts {"text": "...", "language": "..."} and returns
// {"response": {"speech": {"plain": {"speech": "..."}}}, "conversation_id": "..."}.
// We only consume the spoken text.
//
// The skill needs configuration to be useful:
//   - URL: base of the HA instance (e.g. "https://ha.example.com:8123")
//   - Token: a Long-Lived Access Token (HA → Profile → Long-Lived
//     Access Tokens). Stored via internal/secrets / Doppler — not
//     committed.
//
// Without a configured URL+Token the skill returns Action="silent" so
// the LLM picks up the request instead of failing the pipeline.
//
// The skill matches IntentHomeAssistant (a new shortcuts intent) AND
// is opportunistically called for the broader voice-companion intents
// when LLM-fallback is undesirable — but the latter is host-wired and
// not enforced inside this file.
type HomeAssistantSkill struct {
	client  *http.Client
	baseURL string
	token   string
}

// NewHomeAssistantSkill constructs a skill that targets the given HA
// base URL with the given Long-Lived Access Token. Pass empty values
// to disable the skill — Execute will short-circuit to silent.
func NewHomeAssistantSkill(baseURL, token string) *HomeAssistantSkill {
	return &HomeAssistantSkill{
		client:  &http.Client{Timeout: 5 * time.Second},
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		token:   strings.TrimSpace(token),
	}
}

// WithClient overrides the HTTP client. Test-only.
func (s *HomeAssistantSkill) WithClient(c *http.Client) *HomeAssistantSkill {
	out := *s
	if c != nil {
		out.client = c
	}
	return &out
}

// Intent reports the shortcuts.Intent this skill handles. We use the
// generic HomeAssistant intent; in Phase 4 the resolver lexicon adds
// trigger phrases ("schalte", "switch", "turn on/off", ...) that map
// to this intent.
func (s *HomeAssistantSkill) Intent() shortcuts.Intent { return shortcuts.IntentHomeAssistant }

// Configured reports whether the skill has a URL + token. Hosts can
// use this to decide whether to register the skill in the catalog at
// all, or to flip the UtilityDefinition.Enabled flag dynamically.
func (s *HomeAssistantSkill) Configured() bool {
	return s.baseURL != "" && s.token != ""
}

// Probe verifies the configured base URL + token reach a working HA
// instance. It hits /api/ (the unauthenticated health endpoint when
// the token is valid; the same path returns 401 on bad tokens) and
// returns nil on success or a human-readable error suitable for
// surfacing in the Settings UI. Probe never persists state.
func (s *HomeAssistantSkill) Probe(ctx context.Context) error {
	if s == nil {
		return errors.New("home_assistant: nil skill")
	}
	if s.baseURL == "" {
		return errors.New("home_assistant: URL is empty")
	}
	if s.token == "" {
		return errors.New("home_assistant: token is empty")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+"/api/", http.NoBody)
	if err != nil {
		return fmt.Errorf("home_assistant: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("home_assistant: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // probe response close error is not actionable
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return errors.New("home_assistant: invalid token or forbidden")
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("home_assistant: probe returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
}

// Execute runs the skill against a single ToolCall.
func (s *HomeAssistantSkill) Execute(ctx context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	if !s.Configured() {
		// No config = no skill. Fall through to LLM.
		return silentResult(call.Locale), nil
	}
	utterance := strings.TrimSpace(call.Transcript)
	if utterance == "" {
		utterance = strings.TrimSpace(call.Payload)
	}
	if utterance == "" {
		return silentResult(call.Locale), nil
	}

	speech, ok, err := s.callConversation(ctx, utterance, call.Locale)
	if err != nil {
		return assist.ToolResult{}, fmt.Errorf("home_assistant: %w", err)
	}
	if !ok {
		// HA didn't match an intent — let the LLM try.
		return silentResult(call.Locale), nil
	}
	return assist.ToolResult{
		Text:      speech,
		SpeakText: speech,
		Action:    "execute",
		Locale:    call.Locale,
		Surface:   assist.ResultSurfaceActionAck,
		Kind:      assist.ResultKindUtilityAction,
	}, nil
}

// callConversation performs the actual POST. Returns (speech, true,
// nil) on a matched intent, ("", false, nil) on "no intent matched"
// (HA distinguishes these via response.response_type), and (_, _,
// err) on transport errors.
func (s *HomeAssistantSkill) callConversation(ctx context.Context, text, locale string) (string, bool, error) {
	body, err := json.Marshal(map[string]string{
		"text":     text,
		"language": haLanguage(locale),
	})
	if err != nil {
		return "", false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/api/conversation/process", bytes.NewReader(body))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable

	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		// fall through
	case http.StatusUnauthorized, http.StatusForbidden:
		return "", false, errors.New("home_assistant: invalid token or forbidden")
	default:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", false, fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Response struct {
			Speech struct {
				Plain struct {
					Speech string `json:"speech"`
				} `json:"plain"`
			} `json:"speech"`
			ResponseType string `json:"response_type"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", false, fmt.Errorf("decode: %w", err)
	}
	speech := strings.TrimSpace(payload.Response.Speech.Plain.Speech)
	matched := payload.Response.ResponseType != "" && payload.Response.ResponseType != "error"
	if speech == "" {
		return "", false, nil
	}
	// HA returns ResponseType="action_done" / "query_answer" /
	// "error". Treat "error" + "no_intent_match" speech as
	// no-match so the LLM tries the open-ended path.
	if payload.Response.ResponseType == "error" && containsNoIntent(speech) {
		return speech, false, nil
	}
	return speech, matched || speech != "", nil
}

func containsNoIntent(speech string) bool {
	lower := strings.ToLower(speech)
	for _, marker := range []string{"no intent matched", "ich verstehe das nicht", "sorry"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// haLanguage maps the SpeechKit locale to a HA language code. HA uses
// the same ISO-639-1 codes as Wikipedia, so the mapping mirrors
// wikipediaLanguage; kept separate so future skills can diverge.
func haLanguage(locale string) string {
	switch normalizeLocale(locale) {
	case "de":
		return "de"
	default:
		return "en"
	}
}
