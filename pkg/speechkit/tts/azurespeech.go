package tts

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

const (
	azureSpeechScope        = "foundry tts"
	azureSpeechTTSPath      = "tts/cognitiveservices/v1"
	azureSpeechVoicesPath   = "tts/cognitiveservices/voices/list"
	azureSpeechDefaultVoice = "en-US-Harper:MAI-Voice-2"
	azureSpeechDefaultLang  = "en-US"
	azureSpeechUserAgent    = "kombify-SpeechKit"
	// Every MAI output format this adapter requests is 24 kHz.
	azureSpeechSampleRate = 24000
	azureSpeechMaxAudio   = 50 << 20
	azureSpeechMaxVoices  = 8 << 20
)

// AzureSpeechOpts configures the Azure Speech (MAI-Voice) TTS provider.
type AzureSpeechOpts struct {
	// Host is the resource's custom domain, e.g.
	// "myresource.cognitiveservices.azure.com" (no scheme). Required.
	Host string
	// APIKey is the resource key, sent as Ocp-Apim-Subscription-Key.
	APIKey string
	// BearerToken, when set, wins over APIKey: it is called per request and
	// the token rides "Authorization: Bearer". Hosts set it for Entra sign-in.
	BearerToken speechkit.BearerTokenFunc
	// Voice is a Speech voice short name such as "de-DE-Mia:MAI-Voice-2" or
	// "en-US-Ethan:MAI-Voice-2-Flash"; defaults to "en-US-Harper:MAI-Voice-2".
	Voice string
	// Style is an optional mstts:express-as style ("friendly", "whispering").
	Style string
	// StyleDegree scales Style (0.01–2); only emitted together with Style.
	StyleDegree float64
}

// AzureSpeech implements Provider on the Azure Speech SSML endpoint of a
// Microsoft Foundry resource. Microsoft's MAI-Voice models are served there,
// not on the OpenAI-compatible audio/speech route Foundry uses for gpt-*-tts,
// so this adapter talks to the resource's custom domain directly.
//
// It reports the same provider id as Foundry ("foundry"): routing, preference
// pinning and option manifests key on the provider, not on the wire format.
// Validation is strict by default (public https only).
type AzureSpeech struct {
	apiKey      string
	bearer      speechkit.BearerTokenFunc
	voice       string
	style       string
	styleDegree float64
	Host        string
	Validation  netsec.ValidationOptions
	client      *http.Client
}

// AzureSpeechVoice is one entry of the resource's voices list.
type AzureSpeechVoice struct {
	ShortName        string
	DisplayName      string
	LocalName        string
	Locale           string
	Gender           string
	VoiceType        string
	Status           string
	Styles           []string
	ModelSeries      string
	SecondaryLocales []string
	SampleRateHertz  int
}

// NewAzureSpeech creates an Azure Speech TTS provider for MAI voices.
func NewAzureSpeech(opts AzureSpeechOpts) *AzureSpeech {
	voice := strings.TrimSpace(opts.Voice)
	if voice == "" {
		voice = azureSpeechDefaultVoice
	}
	a := &AzureSpeech{
		apiKey:      opts.APIKey,
		bearer:      opts.BearerToken,
		voice:       voice,
		style:       strings.TrimSpace(opts.Style),
		styleDegree: opts.StyleDegree,
		Host:        strings.TrimSpace(opts.Host),
		// Validation zero-value = strict: public https only.
	}
	a.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 30 * time.Second, DialValidation: &a.Validation})
	return a
}

func (a *AzureSpeech) Name() string { return "foundry" }

func (a *AzureSpeech) Kind() ProviderKind { return ProviderKindDirectProvider }

func (*AzureSpeech) Capabilities() []speechkit.Capability { return ttsCapabilities() }

func (a *AzureSpeech) CloseIdleConnections() {
	if a != nil && a.client != nil {
		a.client.CloseIdleConnections()
	}
}

// Synthesize renders text through SSML and returns the requested container.
func (a *AzureSpeech) Synthesize(ctx context.Context, text string, opts SynthesizeOpts) (*Result, error) {
	if text == "" {
		return nil, fmt.Errorf("%s: empty text", azureSpeechScope)
	}
	endpoint, err := a.endpoint(azureSpeechTTSPath)
	if err != nil {
		return nil, err
	}

	resolved := ResolveSynthesizeOptions("foundry", "", opts, provideropts.Values{
		provideropts.OptionVoice:       a.voice,
		provideropts.OptionSpeed:       1.0,
		provideropts.OptionAudioFormat: "mp3",
	}, nil)

	voice := strings.TrimSpace(resolved.Voice)
	if voice == "" {
		voice = a.voice
	}
	format, outputFormat := azureSpeechOutputFormat(resolved.Format)
	lang := VoiceLocale(voice)
	if lang == "" {
		lang = azureSpeechDefaultLang
	}
	ssml := azureSpeechSSML(lang, voice, a.style, a.styleDegree, azureSpeechRate(resolved.Speed), text)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(ssml))
	if err != nil {
		return nil, fmt.Errorf("%s: create request: %w", azureSpeechScope, err)
	}
	req.Header.Set("Content-Type", "application/ssml+xml")
	req.Header.Set("X-Microsoft-OutputFormat", outputFormat)
	req.Header.Set("User-Agent", azureSpeechUserAgent)
	if _, err := a.authorize(ctx, req); err != nil {
		return nil, err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: request failed: %w", azureSpeechScope, err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, azureSpeechStatusError(azureSpeechScope, resp.StatusCode, errBody)
	}

	audio, err := io.ReadAll(io.LimitReader(resp.Body, azureSpeechMaxAudio))
	if err != nil {
		return nil, fmt.Errorf("%s: read response: %w", azureSpeechScope, err)
	}
	return &Result{
		Audio:      audio,
		Format:     format,
		SampleRate: azureSpeechSampleRate,
		Provider:   "foundry",
		Voice:      voice,
	}, nil
}

// Health lists the voices: free, read-only, and it exercises the same host
// and credential Synthesize uses.
func (a *AzureSpeech) Health(ctx context.Context) error {
	_, err := a.ListVoices(ctx)
	return err
}

// azureSpeechVoiceJSON mirrors the voices list entry. SampleRateHertz arrives
// as a string ("24000") and is decoded leniently.
type azureSpeechVoiceJSON struct {
	Name                string         `json:"Name"`
	DisplayName         string         `json:"DisplayName"`
	LocalName           string         `json:"LocalName"`
	ShortName           string         `json:"ShortName"`
	Gender              string         `json:"Gender"`
	Locale              string         `json:"Locale"`
	LocaleName          string         `json:"LocaleName"`
	StyleList           []string       `json:"StyleList"`
	SecondaryLocaleList []string       `json:"SecondaryLocaleList"`
	SampleRateHertz     azureSpeechInt `json:"SampleRateHertz"`
	VoiceType           string         `json:"VoiceType"`
	Status              string         `json:"Status"`
	VoiceTag            struct {
		ModelSeries []string `json:"ModelSeries"`
		PersonaID   []string `json:"PersonaId"`
		Source      []string `json:"Source"`
	} `json:"VoiceTag"`
}

// azureSpeechInt decodes a JSON number that may be quoted. A malformed value
// becomes 0 instead of failing the whole voices list.
type azureSpeechInt int

func (n *azureSpeechInt) UnmarshalJSON(data []byte) error {
	raw := strings.Trim(strings.TrimSpace(string(data)), `"`)
	value, err := strconv.Atoi(raw)
	if err != nil {
		*n = 0
		return nil
	}
	*n = azureSpeechInt(value)
	return nil
}

// ListVoices fetches the voices the resource can synthesize with, MAI and
// classic neural alike; callers filter on IsMAIVoice or ModelSeries.
func (a *AzureSpeech) ListVoices(ctx context.Context) ([]AzureSpeechVoice, error) {
	endpoint, err := a.endpoint(azureSpeechVoicesPath)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%s voices: create request: %w", azureSpeechScope, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", azureSpeechUserAgent)
	usedBearer, err := a.authorize(ctx, req)
	if err != nil {
		return nil, err
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s voices: %w", azureSpeechScope, err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, azureSpeechAuthFailure(azureSpeechScope+" voices", resp.StatusCode, usedBearer)
	default:
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, azureSpeechStatusError(azureSpeechScope+" voices", resp.StatusCode, errBody)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, azureSpeechMaxVoices))
	if err != nil {
		return nil, fmt.Errorf("%s voices: read response: %w", azureSpeechScope, err)
	}
	var entries []azureSpeechVoiceJSON
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("%s voices: parse response: %w", azureSpeechScope, err)
	}
	voices := make([]AzureSpeechVoice, 0, len(entries))
	for _, entry := range entries {
		voice := AzureSpeechVoice{
			ShortName:        entry.ShortName,
			DisplayName:      entry.DisplayName,
			LocalName:        entry.LocalName,
			Locale:           entry.Locale,
			Gender:           entry.Gender,
			VoiceType:        entry.VoiceType,
			Status:           entry.Status,
			Styles:           entry.StyleList,
			SecondaryLocales: entry.SecondaryLocaleList,
			SampleRateHertz:  int(entry.SampleRateHertz),
		}
		if len(entry.VoiceTag.ModelSeries) > 0 {
			voice.ModelSeries = entry.VoiceTag.ModelSeries[0]
		}
		voices = append(voices, voice)
	}
	return voices, nil
}

// MAIVoiceFamilies lists the MAI model series a voice short name can end in.
func MAIVoiceFamilies() []string {
	return []string{"MAI-Voice-2", "MAI-Voice-2-Flash"}
}

// IsMAIVoice reports whether shortName addresses a Microsoft MAI voice
// ("de-DE-Mia:MAI-Voice-2") rather than a classic neural voice.
func IsMAIVoice(shortName string) bool {
	return strings.Contains(shortName, ":MAI-Voice")
}

// VoiceLocale returns the BCP-47 locale a Speech voice short name starts
// with: "de-DE" for "de-DE-Mia:MAI-Voice-2". Empty when the name carries no
// language-region prefix.
func VoiceLocale(shortName string) string {
	name := strings.TrimSpace(shortName)
	if idx := strings.IndexByte(name, ':'); idx >= 0 {
		name = name[:idx]
	}
	parts := strings.Split(name, "-")
	if len(parts) < 2 {
		return ""
	}
	lang, region := parts[0], parts[1]
	if !azureSpeechIsLanguageSubtag(lang) || !azureSpeechIsRegionSubtag(region) {
		return ""
	}
	return lang + "-" + region
}

func azureSpeechIsLanguageSubtag(s string) bool {
	if len(s) < 2 || len(s) > 3 {
		return false
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

// azureSpeechIsRegionSubtag accepts ISO 3166 alpha-2, UN M.49 numeric and,
// defensively, 4-letter script subtags.
func azureSpeechIsRegionSubtag(s string) bool {
	switch len(s) {
	case 2, 4:
		for _, r := range s {
			if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
				return false
			}
		}
		return true
	case 3:
		for _, r := range s {
			if r < '0' || r > '9' {
				return false
			}
		}
		return true
	}
	return false
}

// azureSpeechRate turns a speed multiplier into the SSML prosody rate in
// percent (0 means "omit prosody"). Speed is clamped to 0.5–2.0.
func azureSpeechRate(speed float64) int {
	if speed <= 0 {
		speed = 1.0
	}
	speed = math.Min(math.Max(speed, 0.5), 2.0)
	return int(math.Round((speed - 1) * 100))
}

// azureSpeechOutputFormat maps the generic format to the normalized name and
// the X-Microsoft-OutputFormat value. Unknown formats fall back to mp3.
func azureSpeechOutputFormat(format string) (normalized, header string) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "wav":
		return "wav", "riff-24khz-16bit-mono-pcm"
	case "pcm":
		return "pcm", "raw-24khz-16bit-mono-pcm"
	case "opus":
		return "opus", "ogg-24khz-16bit-mono-opus"
	default:
		return "mp3", "audio-24khz-96kbitrate-mono-mp3"
	}
}

// azureSpeechSSML builds the speak document. Text and attribute values are
// XML-escaped; express-as and prosody are only emitted when they change
// something so the default request stays minimal.
func azureSpeechSSML(lang, voice, style string, styleDegree float64, rate int, text string) string {
	var b strings.Builder
	b.WriteString(`<speak version="1.0" xmlns="http://www.w3.org/2001/10/synthesis" xmlns:mstts="https://www.w3.org/2001/mstts" xml:lang="`)
	azureSpeechEscape(&b, lang)
	b.WriteString(`"><voice name="`)
	azureSpeechEscape(&b, voice)
	b.WriteString(`">`)
	if style != "" {
		b.WriteString(`<mstts:express-as style="`)
		azureSpeechEscape(&b, style)
		b.WriteString(`"`)
		if styleDegree > 0 {
			degree := math.Min(math.Max(styleDegree, 0.01), 2.0)
			b.WriteString(` styledegree="`)
			b.WriteString(strconv.FormatFloat(degree, 'f', -1, 64))
			b.WriteString(`"`)
		}
		b.WriteString(`>`)
	}
	if rate != 0 {
		fmt.Fprintf(&b, `<prosody rate="%+d%%">`, rate)
	}
	azureSpeechEscape(&b, text)
	if rate != 0 {
		b.WriteString(`</prosody>`)
	}
	if style != "" {
		b.WriteString(`</mstts:express-as>`)
	}
	b.WriteString(`</voice></speak>`)
	return b.String()
}

func azureSpeechEscape(b *strings.Builder, s string) {
	// strings.Builder never fails to write.
	_ = xml.EscapeText(b, []byte(s))
}

// authorize attaches the credential and reports which kind was used so
// failures can point at roles versus keys.
func (a *AzureSpeech) authorize(ctx context.Context, req *http.Request) (usedBearer bool, err error) {
	if a.bearer != nil {
		token, err := a.bearer(ctx)
		if err != nil {
			return true, fmt.Errorf("%s: bearer token: %w", azureSpeechScope, err)
		}
		token = strings.TrimSpace(token)
		if token == "" {
			return true, fmt.Errorf("%s: bearer token: empty token", azureSpeechScope)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return true, nil
	}
	if key := strings.TrimSpace(a.apiKey); key != "" {
		req.Header.Set("Ocp-Apim-Subscription-Key", key)
		return false, nil
	}
	return false, fmt.Errorf("%s: no credential configured (API key or bearer token)", azureSpeechScope)
}

// endpoint validates Host through netsec. Host is normally a bare custom
// domain; a full URL is accepted so hosts and tests can point the adapter at
// a non-https loopback server.
func (a *AzureSpeech) endpoint(path string) (string, error) {
	host := strings.TrimSpace(a.Host)
	if host == "" {
		return "", fmt.Errorf("%s: no endpoint configured (set the project endpoint)", azureSpeechScope)
	}
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	endpoint, err := netsec.BuildEndpoint(strings.TrimRight(host, "/"), path, a.Validation)
	if err != nil {
		return "", fmt.Errorf("%s: endpoint: %w", azureSpeechScope, err)
	}
	return endpoint, nil
}

// azureSpeechStatusError classifies a non-200 answer through netsec, so
// bodies never reach logs or UI, and appends Azure's structured message for
// request problems (an unknown voice, a region gap). Auth failures stay opaque.
func azureSpeechStatusError(scope string, status int, body []byte) error {
	base := netsec.ProviderStatusError(scope, status, body)
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return base
	}
	if detail := azureSpeechErrorDetail(body); detail != "" {
		return fmt.Errorf("%w: %s", base, detail)
	}
	return base
}

// azureSpeechErrorDetail extracts code and message from the two envelope
// shapes Azure uses ({"code","message"} and {"error":{"code","message"}}).
func azureSpeechErrorDetail(body []byte) string {
	var envelope struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Error   *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	code, message := envelope.Code, envelope.Message
	if message == "" && envelope.Error != nil {
		code, message = envelope.Error.Code, envelope.Error.Message
	}
	message = strings.Join(strings.Fields(message), " ")
	if message == "" {
		return ""
	}
	if runes := []rune(message); len(runes) > 240 {
		message = string(runes[:240]) + "..."
	}
	if code = strings.TrimSpace(code); code != "" {
		return code + ": " + message
	}
	return message
}

// azureSpeechAuthFailure explains a 401/403 in terms of what the user can
// change: the roles on the resource for a signed-in account, the key otherwise.
func azureSpeechAuthFailure(scope string, status int, usedBearer bool) error {
	if usedBearer {
		return fmt.Errorf("%s: status %d: the signed-in account cannot use this resource; it needs the Cognitive Services User / Foundry User roles and must belong to the resource's tenant", scope, status)
	}
	return fmt.Errorf("%s: status %d: invalid key for this resource (or keys are disabled by policy; sign in with Microsoft instead)", scope, status)
}
