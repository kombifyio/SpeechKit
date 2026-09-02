package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	framework "github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/catalog"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

const defaultUserAgent = "speechkit-client/0.1"

type Options struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
	UserAgent  string
	Timeout    time.Duration
}

type Client struct {
	baseURL   *url.URL
	token     string
	http      *http.Client
	userAgent string
}

func New(opts Options) (*Client, error) {
	raw := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if raw == "" {
		raw = "http://localhost:8080"
	}
	baseURL, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse base URL: %w", err)
	}
	if baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("base URL must include scheme and host")
	}
	httpClient := opts.HTTPClient
	if httpClient == nil {
		timeout := opts.Timeout
		if timeout == 0 {
			timeout = 30 * time.Second
		}
		httpClient = &http.Client{Timeout: timeout}
	}
	userAgent := strings.TrimSpace(opts.UserAgent)
	if userAgent == "" {
		userAgent = defaultUserAgent
	}
	return &Client{
		baseURL:   baseURL,
		token:     strings.TrimSpace(opts.Token),
		http:      httpClient,
		userAgent: userAgent,
	}, nil
}

func FromEnv() (*Client, error) {
	return New(Options{
		BaseURL: os.Getenv("SPEECHKIT_SERVER_URL"),
		Token:   firstNonEmpty(os.Getenv("SPEECHKIT_TOKEN"), os.Getenv("SPEECHKIT_SERVER_TOKEN")),
	})
}

func (c *Client) Status(ctx context.Context) (*Status, error) {
	var out Status
	if err := c.DoJSON(ctx, http.MethodGet, "/readyz", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Config(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	if err := c.DoJSON(ctx, http.MethodGet, "/v1/config", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) CatalogReadiness(ctx context.Context) ([]CatalogReadiness, error) {
	var out struct {
		Readiness []CatalogReadiness `json:"readiness"`
	}
	if err := c.DoJSON(ctx, http.MethodGet, "/v1/catalog/readiness", nil, &out); err != nil {
		return nil, err
	}
	return out.Readiness, nil
}

func (c *Client) ProviderReadiness(ctx context.Context, id string) (*CatalogReadiness, error) {
	var out CatalogReadiness
	if err := c.DoJSON(ctx, http.MethodGet, "/v1/catalog/profiles/"+url.PathEscape(strings.TrimSpace(id))+"/readiness", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CatalogProfiles(ctx context.Context, mode string) ([]framework.ProviderProfile, error) {
	path := "/v1/catalog/profiles"
	if strings.TrimSpace(mode) != "" {
		path += "?mode=" + url.QueryEscape(strings.TrimSpace(mode))
	}
	var out struct {
		Profiles []framework.ProviderProfile `json:"profiles"`
	}
	if err := c.DoJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Profiles, nil
}

func (c *Client) CatalogProviders(ctx context.Context) ([]catalog.ProviderMatrixRow, []catalog.ProviderDefault, error) {
	var out struct {
		ProviderMatrix   []catalog.ProviderMatrixRow `json:"provider_matrix"`
		ProviderDefaults []catalog.ProviderDefault   `json:"provider_defaults"`
	}
	if err := c.DoJSON(ctx, http.MethodGet, "/v1/catalog/providers", nil, &out); err != nil {
		return nil, nil, err
	}
	return out.ProviderMatrix, out.ProviderDefaults, nil
}

func (c *Client) CatalogContracts(ctx context.Context) ([]framework.ModeContract, error) {
	var out struct {
		Contracts []framework.ModeContract `json:"contracts"`
	}
	if err := c.DoJSON(ctx, http.MethodGet, "/v1/catalog/contracts", nil, &out); err != nil {
		return nil, err
	}
	return out.Contracts, nil
}

func (c *Client) PersonasList(ctx context.Context) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodGet, "/v1/personas", nil)
}

func (c *Client) Personas(ctx context.Context) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodGet, "/v1/personas", nil)
}

func (c *Client) Persona(ctx context.Context, id string) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodGet, "/v1/personas/"+url.PathEscape(strings.TrimSpace(id)), nil)
}

func (c *Client) CreatePersona(ctx context.Context, payload any) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodPost, "/v1/personas", payload)
}

func (c *Client) UpdatePersona(ctx context.Context, id string, payload any) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodPatch, "/v1/personas/"+url.PathEscape(strings.TrimSpace(id)), payload)
}

func (c *Client) DeletePersona(ctx context.Context, id string) error {
	return c.DoJSON(ctx, http.MethodDelete, "/v1/personas/"+url.PathEscape(strings.TrimSpace(id)), nil, nil)
}

func (c *Client) Roles(ctx context.Context) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodGet, "/v1/roles", nil)
}

func (c *Client) Role(ctx context.Context, id string) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodGet, "/v1/roles/"+url.PathEscape(strings.TrimSpace(id)), nil)
}

func (c *Client) CreateRole(ctx context.Context, payload any) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodPost, "/v1/roles", payload)
}

func (c *Client) UpdateRole(ctx context.Context, id string, payload any) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodPatch, "/v1/roles/"+url.PathEscape(strings.TrimSpace(id)), payload)
}

func (c *Client) DeleteRole(ctx context.Context, id string) error {
	return c.DoJSON(ctx, http.MethodDelete, "/v1/roles/"+url.PathEscape(strings.TrimSpace(id)), nil, nil)
}

func (c *Client) Sequences(ctx context.Context) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodGet, "/v1/sequences", nil)
}

func (c *Client) Sequence(ctx context.Context, id string) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodGet, "/v1/sequences/"+url.PathEscape(strings.TrimSpace(id)), nil)
}

func (c *Client) CreateSequence(ctx context.Context, payload any) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodPost, "/v1/sequences", payload)
}

func (c *Client) UpdateSequence(ctx context.Context, id string, payload any) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodPatch, "/v1/sequences/"+url.PathEscape(strings.TrimSpace(id)), payload)
}

func (c *Client) DeleteSequence(ctx context.Context, id string) error {
	return c.DoJSON(ctx, http.MethodDelete, "/v1/sequences/"+url.PathEscape(strings.TrimSpace(id)), nil, nil)
}

func (c *Client) VocabularyEntries(ctx context.Context, language string) ([]DictionaryEntry, error) {
	path := "/v1/vocabulary/dictionary"
	if strings.TrimSpace(language) != "" {
		path += "?language=" + url.QueryEscape(strings.TrimSpace(language))
	}
	var out struct {
		Entries []DictionaryEntry `json:"entries"`
	}
	if err := c.DoJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Entries, nil
}

func (c *Client) ReplaceVocabularyEntries(ctx context.Context, language string, entries []DictionaryEntry) ([]DictionaryEntry, error) {
	var out struct {
		Entries []DictionaryEntry `json:"entries"`
	}
	if err := c.DoJSON(ctx, http.MethodPost, "/v1/vocabulary/dictionary", map[string]any{
		"language": strings.TrimSpace(language),
		"entries":  entries,
	}, &out); err != nil {
		return nil, err
	}
	return out.Entries, nil
}

func (c *Client) Words(ctx context.Context, language string) ([]Word, error) {
	path := "/v1/words"
	if strings.TrimSpace(language) != "" {
		path += "?language=" + url.QueryEscape(strings.TrimSpace(language))
	}
	var out struct {
		Words []Word `json:"words"`
	}
	if err := c.DoJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Words, nil
}

func (c *Client) ReplaceWords(ctx context.Context, language string, words []Word) ([]Word, error) {
	var out struct {
		Words []Word `json:"words"`
	}
	if err := c.DoJSON(ctx, http.MethodPost, "/v1/words", map[string]any{
		"language": strings.TrimSpace(language),
		"words":    words,
	}, &out); err != nil {
		return nil, err
	}
	return out.Words, nil
}

func (c *Client) Replacements(ctx context.Context, language string) ([]Replacement, error) {
	path := "/v1/replacements"
	if strings.TrimSpace(language) != "" {
		path += "?language=" + url.QueryEscape(strings.TrimSpace(language))
	}
	var out struct {
		Replacements []Replacement `json:"replacements"`
	}
	if err := c.DoJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Replacements, nil
}

func (c *Client) ReplaceReplacements(ctx context.Context, language string, replacements []Replacement) ([]Replacement, error) {
	var out struct {
		Replacements []Replacement `json:"replacements"`
	}
	if err := c.DoJSON(ctx, http.MethodPost, "/v1/replacements", map[string]any{
		"language":     strings.TrimSpace(language),
		"replacements": replacements,
	}, &out); err != nil {
		return nil, err
	}
	return out.Replacements, nil
}

func (c *Client) Lexicons(ctx context.Context, language string) ([]Lexicon, error) {
	path := "/v1/lexicons"
	if strings.TrimSpace(language) != "" {
		path += "?language=" + url.QueryEscape(strings.TrimSpace(language))
	}
	var out struct {
		Lexicons []Lexicon `json:"lexicons"`
	}
	if err := c.DoJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Lexicons, nil
}

func (c *Client) ReplaceLexicons(ctx context.Context, language string, lexicons []Lexicon) ([]Lexicon, error) {
	var out struct {
		Lexicons []Lexicon `json:"lexicons"`
	}
	if err := c.DoJSON(ctx, http.MethodPost, "/v1/lexicons", map[string]any{
		"language": strings.TrimSpace(language),
		"lexicons": lexicons,
	}, &out); err != nil {
		return nil, err
	}
	return out.Lexicons, nil
}

func (c *Client) Rulesets(ctx context.Context, language string) ([]Ruleset, error) {
	path := "/v1/rulesets"
	if strings.TrimSpace(language) != "" {
		path += "?language=" + url.QueryEscape(strings.TrimSpace(language))
	}
	var out struct {
		Rulesets []Ruleset `json:"rulesets"`
	}
	if err := c.DoJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Rulesets, nil
}

func (c *Client) ReplaceRulesets(ctx context.Context, language string, rulesets []Ruleset) ([]Ruleset, error) {
	var out struct {
		Rulesets []Ruleset `json:"rulesets"`
	}
	if err := c.DoJSON(ctx, http.MethodPost, "/v1/rulesets", map[string]any{
		"language": strings.TrimSpace(language),
		"rulesets": rulesets,
	}, &out); err != nil {
		return nil, err
	}
	return out.Rulesets, nil
}

func (c *Client) CustomizationPack(ctx context.Context, language string) (*Pack, error) {
	path := "/v1/customization/pack"
	if strings.TrimSpace(language) != "" {
		path += "?language=" + url.QueryEscape(strings.TrimSpace(language))
	}
	var out Pack
	if err := c.DoJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ImportCustomizationPack(ctx context.Context, pack Pack) error {
	return c.DoJSON(ctx, http.MethodPost, "/v1/customization/pack", pack, nil)
}

func (c *Client) Transcripts(ctx context.Context, limit int) ([]Transcript, error) {
	path := "/v1/transcripts"
	if limit > 0 {
		path += fmt.Sprintf("?limit=%d", limit)
	}
	var out struct {
		Transcripts []Transcript `json:"transcripts"`
	}
	if err := c.DoJSON(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Transcripts, nil
}

func (c *Client) Transcript(ctx context.Context, id int64) (*Transcript, error) {
	var out Transcript
	if err := c.DoJSON(ctx, http.MethodGet, fmt.Sprintf("/v1/transcripts/%d", id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) VoiceAgentSessionTranscript(ctx context.Context, id int64) (*VoiceAgentTranscript, error) {
	var out VoiceAgentTranscript
	if err := c.DoJSON(ctx, http.MethodGet, fmt.Sprintf("/v1/voiceagent/sessions/%d/transcript", id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) VoiceAgentSessionSummary(ctx context.Context, id int64) (*VoiceAgentSummary, error) {
	var out VoiceAgentSummary
	if err := c.DoJSON(ctx, http.MethodGet, fmt.Sprintf("/v1/voiceagent/sessions/%d/summary", id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) TTSSynthesize(ctx context.Context, input TTSSynthesizeRequest) (*TTSSynthesizeResponse, error) {
	var out TTSSynthesizeResponse
	if err := c.DoJSON(ctx, http.MethodPost, "/v1/tts/synthesize", input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) TTSVoices(ctx context.Context) ([]Voice, error) {
	var out struct {
		Voices []Voice `json:"voices"`
	}
	if err := c.DoJSON(ctx, http.MethodGet, "/v1/tts/voices", nil, &out); err != nil {
		return nil, err
	}
	return out.Voices, nil
}

func (c *Client) TranscribeFile(ctx context.Context, path string, opts TranscribeOptions) (*TranscribeResponse, error) {
	audio, err := os.Open(path) // #nosec G304 -- path is caller-supplied input intended to be uploaded; the client is a Go SDK consumed by trusted callers and the file is read-only-streamed into a multipart body, never executed.
	if err != nil {
		return nil, fmt.Errorf("open audio file: %w", err)
	}
	defer audio.Close() //nolint:errcheck // close error is not actionable for read-only upload

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("audio", filepath.Base(path))
	if err != nil {
		return nil, fmt.Errorf("create multipart file: %w", err)
	}
	if _, err := io.Copy(part, audio); err != nil {
		return nil, fmt.Errorf("read audio file: %w", err)
	}
	writeFormField(writer, "language", opts.Language)
	writeFormField(writer, "model", opts.Model)
	writeFormField(writer, "prompt", opts.Prompt)
	writeSpeakerFormFields(writer, opts.Speaker)
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("finish multipart request: %w", err)
	}

	var out TranscribeResponse
	if err := c.do(ctx, http.MethodPost, "/v1/dictation/transcribe", writer.FormDataContentType(), &body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RawJSON(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.DoJSON(ctx, method, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) DoJSON(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	contentType := ""
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(buf)
		contentType = "application/json"
	}
	return c.do(ctx, method, path, contentType, reader, out)
}

func (c *Client) do(ctx context.Context, method, path, contentType string, body io.Reader, out any) error {
	if c == nil {
		return fmt.Errorf("nil SpeechKit client")
	}
	u := c.resolve(path)
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // body close after full read
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return HTTPError{StatusCode: resp.StatusCode, Body: string(raw)}
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if rawMessage, ok := out.(*json.RawMessage); ok {
		*rawMessage = append((*rawMessage)[:0], raw...)
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) resolve(path string) string {
	ref, err := url.Parse(path)
	if err != nil {
		return c.baseURL.String()
	}
	return c.baseURL.ResolveReference(ref).String()
}

func writeFormField(w *multipart.Writer, key, value string) {
	if strings.TrimSpace(value) != "" {
		_ = w.WriteField(key, value)
	}
}

func writeSpeakerFormFields(w *multipart.Writer, opts speaker.Options) {
	opts = opts.Normalized()
	if !opts.WantsDiarization() {
		return
	}
	_ = w.WriteField("speaker_enabled", "true")
	if opts.Diarization || opts.Enabled {
		_ = w.WriteField("speaker_diarization", "true")
	}
	if opts.Identification {
		_ = w.WriteField("speaker_identification", "true")
	}
	if opts.Attribution {
		_ = w.WriteField("speaker_attribution", "true")
	}
	writeFormField(w, "speaker_provider_profile_id", opts.ProviderProfileID)
	writeFormField(w, "speaker_model", opts.Model)
	writeFormField(w, "speaker_diarization_model", opts.DiarizationModel)
	writeFormField(w, "speaker_type", opts.SpeakerType)
	if opts.SpeakersExpected > 0 {
		_ = w.WriteField("speakers_expected", fmt.Sprint(opts.SpeakersExpected))
	}
	if opts.MinSpeakersExpected > 0 {
		_ = w.WriteField("speaker_min", fmt.Sprint(opts.MinSpeakersExpected))
	}
	if opts.MaxSpeakersExpected > 0 {
		_ = w.WriteField("speaker_max", fmt.Sprint(opts.MaxSpeakersExpected))
	}
	if len(opts.KnownValues) > 0 {
		_ = w.WriteField("speaker_known_values", strings.Join(opts.KnownValues, ","))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
