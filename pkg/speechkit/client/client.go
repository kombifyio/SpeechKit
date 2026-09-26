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

// Options configures [New]. Every field is optional: an empty BaseURL means
// http://localhost:8080, an empty Token sends no Authorization header, a nil
// HTTPClient is replaced by one whose timeout is Timeout (30s when zero), and
// an empty UserAgent uses the package default. Timeout is ignored when
// HTTPClient is set.
type Options struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
	UserAgent  string
	Timeout    time.Duration
}

// Client is a typed caller for one SpeechKit Server. Every request carries
// Accept: application/json, the configured User-Agent and, when a token was
// given, a bearer Authorization header; non-2xx responses surface as
// [HTTPError]. A Client is immutable after [New] and safe for concurrent use.
type Client struct {
	baseURL   *url.URL
	token     string
	http      *http.Client
	userAgent string
}

// New builds a [Client] from opts, applying the defaults described on
// [Options]. It performs no network I/O and returns an error only when
// BaseURL cannot be parsed or lacks a scheme or host.
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

// FromEnv builds a [Client] from the environment: SPEECHKIT_SERVER_URL as the
// base URL (default http://localhost:8080) and SPEECHKIT_TOKEN, or failing
// that SPEECHKIT_SERVER_TOKEN, as the bearer token. It fails as [New] does.
func FromEnv() (*Client, error) {
	return New(Options{
		BaseURL: os.Getenv("SPEECHKIT_SERVER_URL"),
		Token:   firstNonEmpty(os.Getenv("SPEECHKIT_TOKEN"), os.Getenv("SPEECHKIT_SERVER_TOKEN")),
	})
}

// Status calls GET /readyz and decodes the readiness report. The server
// answers 503 while any blocking component is not "ok", so a not-ready server
// surfaces as an [HTTPError] (its Body still holds the JSON report) and a nil
// error means the server is ready to serve its enabled modes.
func (c *Client) Status(ctx context.Context) (*Status, error) {
	var out Status
	if err := c.DoJSON(ctx, http.MethodGet, "/readyz", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Config calls GET /v1/config and returns the server's redacted configuration
// summary (version, public_url, modes, auth_mode, feature flags, request
// limits and ready_status) as a generic JSON object; the key set is defined
// by the server, not by this package.
func (c *Client) Config(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	if err := c.DoJSON(ctx, http.MethodGet, "/v1/config", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CatalogReadiness calls GET /v1/catalog/readiness and returns one entry per
// provider profile in the server's default catalog, stating whether that
// profile is enabled, has credentials and can serve requests right now.
func (c *Client) CatalogReadiness(ctx context.Context) ([]CatalogReadiness, error) {
	var out struct {
		Readiness []CatalogReadiness `json:"readiness"`
	}
	if err := c.DoJSON(ctx, http.MethodGet, "/v1/catalog/readiness", nil, &out); err != nil {
		return nil, err
	}
	return out.Readiness, nil
}

// ProviderReadiness calls GET /v1/catalog/profiles/{id}/readiness for one
// provider profile ID (whitespace-trimmed and URL-escaped). An unknown
// profile surfaces as an [HTTPError] with status 404.
func (c *Client) ProviderReadiness(ctx context.Context, id string) (*CatalogReadiness, error) {
	var out CatalogReadiness
	if err := c.DoJSON(ctx, http.MethodGet, "/v1/catalog/profiles/"+url.PathEscape(strings.TrimSpace(id))+"/readiness", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CatalogProfiles calls GET /v1/catalog/profiles and returns the server's
// provider profiles. An empty mode returns every profile; otherwise mode must
// be "dictation", "assist", "voiceagent" or "voice_agent" and only that
// mode's profiles are returned (unknown modes yield a 400 [HTTPError]).
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

// CatalogProviders calls GET /v1/catalog/providers and returns the provider
// feature matrix (one row per provider with its profiles and per-feature
// support) alongside the flat list of provider default profiles.
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

// CatalogContracts calls GET /v1/catalog/contracts and returns the mode
// contracts (input, output, allowed and forbidden capabilities per mode) that
// the server enforces.
func (c *Client) CatalogContracts(ctx context.Context) ([]framework.ModeContract, error) {
	var out struct {
		Contracts []framework.ModeContract `json:"contracts"`
	}
	if err := c.DoJSON(ctx, http.MethodGet, "/v1/catalog/contracts", nil, &out); err != nil {
		return nil, err
	}
	return out.Contracts, nil
}

// PersonasList calls GET /v1/personas and returns the raw JSON envelope; it
// is equivalent to [Client.Personas].
func (c *Client) PersonasList(ctx context.Context) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodGet, "/v1/personas", nil)
}

// Personas calls GET /v1/personas and returns the raw JSON envelope
// ({"personas": [...]}) so hosts can decode the "personas" array into their
// own persona type or into []PersonaResource.
func (c *Client) Personas(ctx context.Context) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodGet, "/v1/personas", nil)
}

// Persona calls GET /v1/personas/{id} and returns one persona as raw JSON. The
// id is whitespace-trimmed and URL-escaped; an unknown persona surfaces as an
// [HTTPError] with status 404.
func (c *Client) Persona(ctx context.Context, id string) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodGet, "/v1/personas/"+url.PathEscape(strings.TrimSpace(id)), nil)
}

// CreatePersona posts payload, any JSON-marshalable persona definition, to
// /v1/personas and returns the stored persona as raw JSON. The server needs an
// admin identity (403 otherwise) and rejects invalid definitions with 400.
func (c *Client) CreatePersona(ctx context.Context, payload any) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodPost, "/v1/personas", payload)
}

// UpdatePersona sends payload as a PATCH to /v1/personas/{id} and returns the
// stored persona as raw JSON. The server treats payload as the complete
// definition (an upsert keyed by the path id, not a field-wise merge) and
// ignores any id inside payload.
func (c *Client) UpdatePersona(ctx context.Context, id string, payload any) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodPatch, "/v1/personas/"+url.PathEscape(strings.TrimSpace(id)), payload)
}

// DeletePersona calls DELETE /v1/personas/{id}. Unlike
// [Client.DeleteVoiceAgentSession] it does not treat 404 as success; deleting
// an unknown persona returns an [HTTPError].
func (c *Client) DeletePersona(ctx context.Context, id string) error {
	return c.DoJSON(ctx, http.MethodDelete, "/v1/personas/"+url.PathEscape(strings.TrimSpace(id)), nil, nil)
}

// Roles calls GET /v1/roles and returns the raw JSON envelope
// ({"roles": [...]}); see [Client.Personas] for the decoding pattern.
func (c *Client) Roles(ctx context.Context) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodGet, "/v1/roles", nil)
}

// Role calls GET /v1/roles/{id} and returns one role as raw JSON; an unknown
// id surfaces as an [HTTPError] with status 404.
func (c *Client) Role(ctx context.Context, id string) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodGet, "/v1/roles/"+url.PathEscape(strings.TrimSpace(id)), nil)
}

// CreateRole posts payload, any JSON-marshalable role definition, to /v1/roles
// and returns the stored role as raw JSON. The server needs an admin identity
// (403 otherwise) and rejects invalid definitions with 400.
func (c *Client) CreateRole(ctx context.Context, payload any) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodPost, "/v1/roles", payload)
}

// UpdateRole sends payload as a PATCH to /v1/roles/{id} and returns the stored
// role as raw JSON. As with [Client.UpdatePersona], payload is the complete
// definition and the path id wins over any id inside it.
func (c *Client) UpdateRole(ctx context.Context, id string, payload any) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodPatch, "/v1/roles/"+url.PathEscape(strings.TrimSpace(id)), payload)
}

// DeleteRole calls DELETE /v1/roles/{id}; deleting an unknown role returns an
// [HTTPError] with status 404.
func (c *Client) DeleteRole(ctx context.Context, id string) error {
	return c.DoJSON(ctx, http.MethodDelete, "/v1/roles/"+url.PathEscape(strings.TrimSpace(id)), nil, nil)
}

// Sequences calls GET /v1/sequences and returns the raw JSON envelope
// ({"sequences": [...]}); see [Client.Personas] for the decoding pattern.
func (c *Client) Sequences(ctx context.Context) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodGet, "/v1/sequences", nil)
}

// Sequence calls GET /v1/sequences/{id} and returns one sequence as raw JSON;
// an unknown id surfaces as an [HTTPError] with status 404.
func (c *Client) Sequence(ctx context.Context, id string) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodGet, "/v1/sequences/"+url.PathEscape(strings.TrimSpace(id)), nil)
}

// CreateSequence posts payload, any JSON-marshalable sequence definition, to
// /v1/sequences and returns the stored sequence as raw JSON. The server needs
// an admin identity (403 otherwise) and rejects invalid definitions with 400.
func (c *Client) CreateSequence(ctx context.Context, payload any) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodPost, "/v1/sequences", payload)
}

// UpdateSequence sends payload as a PATCH to /v1/sequences/{id} and returns
// the stored sequence as raw JSON. As with [Client.UpdatePersona], payload is
// the complete definition and the path id wins over any id inside it.
func (c *Client) UpdateSequence(ctx context.Context, id string, payload any) (json.RawMessage, error) {
	return c.RawJSON(ctx, http.MethodPatch, "/v1/sequences/"+url.PathEscape(strings.TrimSpace(id)), payload)
}

// DeleteSequence calls DELETE /v1/sequences/{id}; deleting an unknown
// sequence returns an [HTTPError] with status 404.
func (c *Client) DeleteSequence(ctx context.Context, id string) error {
	return c.DoJSON(ctx, http.MethodDelete, "/v1/sequences/"+url.PathEscape(strings.TrimSpace(id)), nil, nil)
}

// VocabularyEntries calls GET /v1/vocabulary/dictionary and returns the
// enabled user-dictionary entries visible to the caller. A non-empty language
// (whitespace-trimmed) restricts the result to that language plus
// language-neutral entries; an empty language returns every language.
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

// ReplaceVocabularyEntries posts to /v1/vocabulary/dictionary, replacing the
// caller's stored dictionary for language with entries in one transaction,
// and returns the entries as persisted. Entries without Spoken or Canonical
// are dropped and an entry with an empty Language inherits language. The
// server requires an admin identity (403 otherwise).
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

// Words calls GET /v1/words and returns the caller's enabled customization
// Words (vocabulary bias terms), limited to language when it is non-empty.
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

// ReplaceWords posts to /v1/words, replacing the caller's stored Words for
// language in one transaction, and returns the list as persisted. The server
// requires an admin identity (403 otherwise).
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

// Replacements calls GET /v1/replacements and returns the caller's enabled
// Replacements (post-recognition rewrite rules), limited to language when it
// is non-empty.
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

// ReplaceReplacements posts to /v1/replacements, replacing the caller's stored
// Replacements for language in one transaction, and returns the list as
// persisted. The server requires an admin identity (403 otherwise).
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

// Lexicons calls GET /v1/lexicons and returns the caller's enabled Lexicons
// (named groups of Word IDs), limited to language when it is non-empty.
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

// ReplaceLexicons posts to /v1/lexicons, replacing the caller's stored
// Lexicons for language in one transaction, and returns the list as
// persisted. The server requires an admin identity (403 otherwise).
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

// Rulesets calls GET /v1/rulesets and returns the caller's enabled Rulesets
// (named groups of Replacement IDs), limited to language when it is
// non-empty.
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

// ReplaceRulesets posts to /v1/rulesets, replacing the caller's stored
// Rulesets for language in one transaction, and returns the list as
// persisted. The server requires an admin identity (403 otherwise).
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

// CustomizationPack calls GET /v1/customization/pack and returns the caller's
// Words, Replacements, Lexicons and Rulesets bundled as one exportable [Pack]
// with SchemaVersion and CreatedAt set by the server. A non-empty language
// limits the bundle to that language.
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

// ImportCustomizationPack posts pack to /v1/customization/pack. The server
// validates it, then for each language present in the pack replaces the
// caller's stored Words, Replacements, Lexicons and Rulesets with the pack's
// items; languages absent from the pack are left untouched. It requires an
// admin identity (403 otherwise) and rejects invalid packs with 400.
func (c *Client) ImportCustomizationPack(ctx context.Context, pack Pack) error {
	return c.DoJSON(ctx, http.MethodPost, "/v1/customization/pack", pack, nil)
}

// Transcripts calls GET /v1/transcripts and returns the caller's persisted
// dictation transcripts. limit > 0 is sent as ?limit= and must be at most 200
// (the server rejects larger values with 400); limit <= 0 omits the parameter
// and yields the server default of 50.
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

// Transcript calls GET /v1/transcripts/{id} and returns one persisted
// transcript. An unknown id surfaces as an [HTTPError] with status 404 and a
// transcript owned by another caller as 403.
func (c *Client) Transcript(ctx context.Context, id int64) (*Transcript, error) {
	var out Transcript
	if err := c.DoJSON(ctx, http.MethodGet, fmt.Sprintf("/v1/transcripts/%d", id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// VoiceAgentSessionTranscript calls GET /v1/voiceagent/sessions/{id}/transcript
// and returns the stored transcript and turns of a finished Voice Agent
// session. id is the numeric store record id, not the live session's string
// [VoiceAgentTicket.SessionID]; unknown ids yield a 404 [HTTPError].
func (c *Client) VoiceAgentSessionTranscript(ctx context.Context, id int64) (*VoiceAgentTranscript, error) {
	var out VoiceAgentTranscript
	if err := c.DoJSON(ctx, http.MethodGet, fmt.Sprintf("/v1/voiceagent/sessions/%d/transcript", id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// VoiceAgentSessionSummary calls GET /v1/voiceagent/sessions/{id}/summary and
// returns the stored structured summary of a finished Voice Agent session. id
// is the numeric store record id, not the live session's string
// [VoiceAgentTicket.SessionID]; unknown ids yield a 404 [HTTPError].
func (c *Client) VoiceAgentSessionSummary(ctx context.Context, id int64) (*VoiceAgentSummary, error) {
	var out VoiceAgentSummary
	if err := c.DoJSON(ctx, http.MethodGet, fmt.Sprintf("/v1/voiceagent/sessions/%d/summary", id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TTSSynthesize posts input to /v1/tts/synthesize and returns the synthesized
// audio. The server rejects an empty Text with 400 and answers 503 when no
// configured TTS provider can serve the request.
func (c *Client) TTSSynthesize(ctx context.Context, input TTSSynthesizeRequest) (*TTSSynthesizeResponse, error) {
	var out TTSSynthesizeResponse
	if err := c.DoJSON(ctx, http.MethodPost, "/v1/tts/synthesize", input, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TTSVoices calls GET /v1/tts/voices and returns the voices the server's
// enabled TTS providers are configured to use. It is a configuration
// snapshot, not live provider voice discovery, and is empty when no TTS
// provider is enabled.
func (c *Client) TTSVoices(ctx context.Context) ([]Voice, error) {
	var out struct {
		Voices []Voice `json:"voices"`
	}
	if err := c.DoJSON(ctx, http.MethodGet, "/v1/tts/voices", nil, &out); err != nil {
		return nil, err
	}
	return out.Voices, nil
}

// TranscribeFile uploads the audio file at path as a multipart/form-data POST
// to /v1/dictation/transcribe and returns the transcript. The file is read
// fully into memory first and is subject to the server's upload limit (25 MB
// by default). Empty opts fields are omitted from the form, and speaker
// fields are sent only when opts.Speaker asks for diarization.
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

// RawJSON performs a request exactly like [Client.DoJSON] but returns the
// undecoded 2xx response body, so hosts can call endpoints this package has
// no typed method for. A response without a body yields a nil message.
func (c *Client) RawJSON(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.DoJSON(ctx, method, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// DoJSON sends an HTTP request with method to path (resolved against the base
// URL; it may carry a query string) and decodes a 2xx response body into out.
// A non-nil body is JSON-encoded and sent as application/json. out may be
// nil to discard the response or a *json.RawMessage to receive it verbatim;
// an empty body leaves out unchanged. Non-2xx statuses return [HTTPError]
// carrying the raw body, and a nil receiver returns an error.
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
