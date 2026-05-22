package voice_companion

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/assist"
	"github.com/kombifyio/SpeechKit/internal/shortcuts"
)

// WikipediaSkill answers "tell me about <topic>" / "erzähl mir was
// ueber <thema>" by calling Wikipedia's free REST summary endpoint
// (https://<lang>.wikipedia.org/api/rest_v1/page/summary/<topic>),
// which returns a one-paragraph extract suitable for TTS playback.
//
// Locale "de" hits de.wikipedia.org, everything else hits
// en.wikipedia.org. The endpoint base is injectable for tests; the
// concrete language host is appended at request time so the same fake
// server can answer DE and EN requests.
type WikipediaSkill struct {
	client      *http.Client
	endpointFmt string
}

// NewWikipediaSkill returns a skill backed by Wikipedia's public REST
// API and a 5s HTTP client.
func NewWikipediaSkill() *WikipediaSkill {
	return &WikipediaSkill{
		client: &http.Client{Timeout: 5 * time.Second},
		// %s = language code, %s = url-encoded topic.
		endpointFmt: "https://%s.wikipedia.org/api/rest_v1/page/summary/%s",
	}
}

// WithClient returns a copy of the skill using the given HTTP client.
func (s *WikipediaSkill) WithClient(c *http.Client) *WikipediaSkill {
	out := *s
	if c != nil {
		out.client = c
	}
	return &out
}

// WithEndpointFormat swaps the URL template. Use for tests:
// "http://127.0.0.1:1234/%s/%s" where the first %s is language, second
// is topic.
func (s *WikipediaSkill) WithEndpointFormat(format string) *WikipediaSkill {
	out := *s
	if format != "" {
		out.endpointFmt = format
	}
	return &out
}

// Intent reports the shortcuts.Intent this skill handles.
func (s *WikipediaSkill) Intent() shortcuts.Intent { return shortcuts.IntentWikipedia }

// Execute runs the skill against a single ToolCall.
func (s *WikipediaSkill) Execute(ctx context.Context, call assist.ToolCall) (assist.ToolResult, error) {
	topic := strings.TrimSpace(call.Payload)
	topic = strings.TrimSuffix(topic, "?")
	topic = strings.TrimSuffix(topic, ".")
	if topic == "" {
		return silentResult(call.Locale), nil
	}

	lang := wikipediaLanguage(call.Locale)
	endpoint := fmt.Sprintf(s.endpointFmt, lang, url.PathEscape(topic))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return assist.ToolResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "kombify-SpeechKit/voice-companion (https://github.com/kombifyio/SpeechKit)")

	resp, err := s.client.Do(req)
	if err != nil {
		return assist.ToolResult{}, fmt.Errorf("wikipedia: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable

	if resp.StatusCode == http.StatusNotFound {
		text := formatTopicNotFound(topic, call.Locale)
		return assist.ToolResult{
			Text:      text,
			SpeakText: text,
			Action:    "respond",
			Locale:    call.Locale,
			Surface:   assist.ResultSurfacePanel,
			Kind:      assist.ResultKindAnswer,
		}, nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return assist.ToolResult{}, fmt.Errorf("wikipedia status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		Title   string `json:"title"`
		Extract string `json:"extract"`
		Type    string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return assist.ToolResult{}, fmt.Errorf("wikipedia decode: %w", err)
	}

	if payload.Type == "disambiguation" {
		// Multiple candidates — better to ask the LLM to disambiguate.
		return silentResult(call.Locale), nil
	}

	extract := condenseExtract(payload.Extract)
	if extract == "" {
		return silentResult(call.Locale), nil
	}
	return assist.ToolResult{
		Text:      extract,
		SpeakText: extract,
		Action:    "respond",
		Locale:    call.Locale,
		Surface:   assist.ResultSurfacePanel,
		Kind:      assist.ResultKindAnswer,
	}, nil
}

// wikipediaLanguage maps the SpeechKit locale to a Wikipedia
// subdomain. DE/EN are the Phase-1 coverage; other locales fall back to
// EN (Wikipedia has the broadest article coverage there).
func wikipediaLanguage(locale string) string {
	switch normalizeLocale(locale) {
	case "de":
		return "de"
	default:
		return "en"
	}
}

// condenseExtract trims the Wikipedia summary to a TTS-friendly length
// (~3 sentences max, capped at 600 chars). Anything longer is
// painful as spoken output.
func condenseExtract(extract string) string {
	extract = strings.TrimSpace(extract)
	if extract == "" {
		return ""
	}
	// Split on sentence terminators while keeping the original
	// terminator. A naive split is fine for the summary endpoint which
	// already returns clean prose.
	sentences := splitSentences(extract)
	if len(sentences) > 3 {
		sentences = sentences[:3]
	}
	out := strings.Join(sentences, " ")
	if len(out) > 600 {
		out = strings.TrimSpace(out[:600]) + "…"
	}
	return out
}

func splitSentences(text string) []string {
	var (
		sentences []string
		current   strings.Builder
	)
	for _, r := range text {
		current.WriteRune(r)
		if r == '.' || r == '!' || r == '?' {
			s := strings.TrimSpace(current.String())
			if s != "" {
				sentences = append(sentences, s)
			}
			current.Reset()
		}
	}
	if remaining := strings.TrimSpace(current.String()); remaining != "" {
		sentences = append(sentences, remaining)
	}
	return sentences
}

func formatTopicNotFound(topic, locale string) string {
	switch normalizeLocale(locale) {
	case "de":
		return fmt.Sprintf("Zu %q habe ich keinen Wikipedia-Eintrag gefunden.", topic)
	default:
		return fmt.Sprintf("I could not find a Wikipedia entry for %q.", topic)
	}
}
