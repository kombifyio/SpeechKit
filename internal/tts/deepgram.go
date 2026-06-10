package tts

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/models"
	"github.com/kombifyio/SpeechKit/internal/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
)

const (
	deepgramTTSBaseURL        = "https://api.deepgram.com"
	deepgramTTSPath           = "v1/speak"
	deepgramTTSDefaultModel   = "aura-2-thalia-en"
	deepgramTTSDefaultDEModel = "aura-2-viktoria-de"
	deepgramTTSSampleRate     = 24000
	deepgramTTSMaxChars       = 1900 // Aura REST hard limit is 2000 chars/request; stay under.
	deepgramTTSMaxAudio       = 64 << 20
)

// Deepgram implements Provider using the Deepgram Aura-2 text-to-speech REST
// API (POST /v1/speak). It reuses the Deepgram API key shared with the STT and
// Voice Agent adapters. Aura voices are selected via the model id
// (e.g. "aura-2-thalia-en"); there is no separate voice parameter.
//
// Aura REST caps a single request at 2000 characters, so longer text is split
// on sentence/word boundaries. Raw concatenation is correct for streaming
// codecs (mp3, linear16/pcm, ogg-opus). WAV is handled specially: a multi-chunk
// WAV request is fetched as linear16 PCM and the joined PCM is wrapped in a
// single RIFF header (see encodeWAV), so the result is always one valid WAV.
type Deepgram struct {
	apiKey     string
	model      string
	BaseURL    string
	Validation netsec.ValidationOptions
	client     *http.Client
}

// DeepgramOpts configures the Deepgram Aura TTS provider.
type DeepgramOpts struct {
	APIKey string
	Model  string // Aura-2 voice id, e.g. "aura-2-thalia-en"; empty => locale default
}

// NewDeepgram creates a Deepgram Aura TTS provider.
func NewDeepgram(opts DeepgramOpts) *Deepgram {
	p := &Deepgram{
		apiKey:  opts.APIKey,
		model:   strings.TrimSpace(opts.Model),
		BaseURL: deepgramTTSBaseURL,
		// Validation zero-value = strict: public https only.
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 45 * time.Second, DialValidation: &p.Validation})
	return p
}

func (d *Deepgram) Name() string { return "deepgram" }

func (d *Deepgram) Kind() models.ProviderKind { return models.ProviderKindDirectProvider }

// resolveModel picks the Aura voice/model for a request: an explicit per-request
// Voice override wins, then the provider's configured model, then a locale
// default (German locales map to a German Aura voice, everything else to the
// English default).
func (d *Deepgram) resolveModel(opts SynthesizeOpts) string {
	if v := strings.TrimSpace(opts.Voice); v != "" {
		return v
	}
	if d.model != "" {
		return d.model
	}
	locale := strings.ToLower(strings.TrimSpace(opts.Locale))
	if strings.HasPrefix(locale, "de") {
		return deepgramTTSDefaultDEModel
	}
	return deepgramTTSDefaultModel
}

// speakParams maps a generic output format to Deepgram /v1/speak query params
// and returns the params plus the normalized format string for the Result.
func speakParams(model, format string) (url.Values, string) {
	q := url.Values{}
	q.Set("model", model)
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "wav":
		q.Set("encoding", "linear16")
		q.Set("sample_rate", fmt.Sprintf("%d", deepgramTTSSampleRate))
		q.Set("container", "wav")
		return q, "wav"
	case "pcm", "linear16":
		q.Set("encoding", "linear16")
		q.Set("sample_rate", fmt.Sprintf("%d", deepgramTTSSampleRate))
		q.Set("container", "none")
		return q, "pcm"
	case "opus":
		q.Set("encoding", "opus")
		q.Set("container", "ogg")
		return q, "opus"
	default: // mp3 is Aura's default container.
		q.Set("encoding", "mp3")
		return q, "mp3"
	}
}

func (d *Deepgram) Synthesize(ctx context.Context, text string, opts SynthesizeOpts) (*Result, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("deepgram tts: empty text")
	}
	if d.apiKey == "" {
		return nil, fmt.Errorf("deepgram tts: no API key configured")
	}

	providerOverrides := provideropts.Values{}
	if strings.TrimSpace(d.model) != "" {
		providerOverrides[provideropts.OptionVoice] = strings.TrimSpace(d.model)
	}
	resolved := ResolveSynthesizeOptions("deepgram", "tts.deepgram.aura-2", opts, provideropts.Values{
		provideropts.OptionAudioFormat: "mp3",
	}, providerOverrides)

	model := d.resolveModel(SynthesizeOpts{Locale: resolved.Locale, Voice: resolved.Voice})
	_, actualFormat := speakParams(model, resolved.Format)
	chunks := splitForDeepgramTTS(text, deepgramTTSMaxChars)

	// Multi-chunk WAV needs special handling: each /v1/speak WAV response is a
	// self-contained RIFF file, so naive concatenation stacks multiple headers
	// into a malformed blob. Fetch the chunks as raw linear16 PCM instead and
	// wrap the joined PCM in one WAV header. Single-chunk WAV and all streaming
	// codecs (mp3, pcm, ogg-opus) concatenate correctly as-is.
	if actualFormat == "wav" && len(chunks) > 1 {
		pcmEndpoint, err := d.buildSpeakEndpoint(model, "pcm")
		if err != nil {
			return nil, err
		}
		var pcm []byte
		for _, chunk := range chunks {
			part, err := d.synthesizeChunk(ctx, pcmEndpoint, chunk)
			if err != nil {
				return nil, err
			}
			pcm = append(pcm, part...)
		}
		return &Result{
			Audio:      encodeAuraWAV(pcm),
			Format:     "wav",
			SampleRate: deepgramTTSSampleRate,
			Provider:   "deepgram",
			Voice:      model,
		}, nil
	}

	endpoint, err := d.buildSpeakEndpoint(model, resolved.Format)
	if err != nil {
		return nil, err
	}
	var audio []byte
	for _, chunk := range chunks {
		part, err := d.synthesizeChunk(ctx, endpoint, chunk)
		if err != nil {
			return nil, err
		}
		audio = append(audio, part...)
	}

	return &Result{
		Audio:      audio,
		Format:     actualFormat,
		SampleRate: deepgramTTSSampleRate,
		Provider:   "deepgram",
		Voice:      model,
	}, nil
}

// buildSpeakEndpoint resolves the /v1/speak request URL for a model + output
// format, applying the provider's endpoint validation.
func (d *Deepgram) buildSpeakEndpoint(model, format string) (string, error) {
	q, _ := speakParams(model, format)
	base := d.BaseURL
	if base == "" {
		base = deepgramTTSBaseURL
	}
	endpoint, err := netsec.BuildEndpoint(base, deepgramTTSPath, d.Validation)
	if err != nil {
		return "", fmt.Errorf("deepgram tts: endpoint: %w", err)
	}
	return endpoint + "?" + q.Encode(), nil
}

func (d *Deepgram) synthesizeChunk(ctx context.Context, endpoint, text string) ([]byte, error) {
	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return nil, fmt.Errorf("deepgram tts: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("deepgram tts: create request: %w", err)
	}
	req.Header.Set("Authorization", "Token "+d.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("deepgram tts: request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, netsec.ProviderStatusError("deepgram tts", resp.StatusCode, errBody)
	}
	audio, err := io.ReadAll(io.LimitReader(resp.Body, deepgramTTSMaxAudio))
	if err != nil {
		return nil, fmt.Errorf("deepgram tts: read response: %w", err)
	}
	return audio, nil
}

func (d *Deepgram) Health(ctx context.Context) error {
	if d.apiKey == "" {
		return fmt.Errorf("deepgram tts: no API key configured")
	}
	_, err := d.Synthesize(ctx, "ok", SynthesizeOpts{Format: "mp3"})
	if err != nil {
		return fmt.Errorf("deepgram tts: health check failed: %w", err)
	}
	return nil
}

func (d *Deepgram) CloseIdleConnections() {
	if d != nil && d.client != nil {
		d.client.CloseIdleConnections()
	}
}

// splitForDeepgramTTS breaks text into chunks no longer than maxChars,
// preferring to break on sentence terminators and then whitespace so synthesis
// boundaries fall on natural pauses. Text already within the limit is returned
// as a single chunk.
func splitForDeepgramTTS(text string, maxChars int) []string {
	if maxChars <= 0 || len(text) <= maxChars {
		return []string{text}
	}
	var chunks []string
	remaining := text
	for len(remaining) > maxChars {
		window := remaining[:maxChars]
		cut := strings.LastIndexAny(window, ".!?")
		if cut < maxChars/2 {
			if sp := strings.LastIndex(window, " "); sp > 0 {
				cut = sp
			} else {
				cut = maxChars - 1
			}
		}
		chunk := strings.TrimSpace(remaining[:cut+1])
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		remaining = strings.TrimSpace(remaining[cut+1:])
	}
	if remaining != "" {
		chunks = append(chunks, remaining)
	}
	return chunks
}

// encodeAuraWAV wraps raw linear16 (16-bit) mono PCM from Deepgram Aura
// (deepgramTTSSampleRate Hz) in a canonical 44-byte WAV/RIFF container. Used for
// multi-chunk synthesis, where each chunk is fetched as headerless PCM and the
// joined stream is given one header. This mirrors internal/audio.PCMToWAV but
// targets Aura's 24 kHz rate (that helper is fixed at the 16 kHz capture rate)
// and lives here so the kernel TTS package keeps no audio-package dependency.
// All header fields are untyped constants except the data length, matching the
// PCMToWAV pattern so only the length conversion needs a gosec annotation.
func encodeAuraWAV(pcm []byte) []byte {
	const (
		channels      = 1
		bitsPerSample = 16
		byteRate      = deepgramTTSSampleRate * channels * bitsPerSample / 8
		blockAlign    = channels * bitsPerSample / 8
	)
	dataSize := uint32(len(pcm)) // #nosec G115 -- Aura output is bounded by deepgramTTSMaxAudio per chunk, far below the 4 GiB WAV RIFF limit.
	out := make([]byte, 44+len(pcm))

	copy(out[0:], "RIFF")
	binary.LittleEndian.PutUint32(out[4:], 36+dataSize)
	copy(out[8:], "WAVE")

	copy(out[12:], "fmt ")
	binary.LittleEndian.PutUint32(out[16:], 16) // PCM fmt chunk size
	binary.LittleEndian.PutUint16(out[20:], 1)  // audio format: PCM
	binary.LittleEndian.PutUint16(out[22:], channels)
	binary.LittleEndian.PutUint32(out[24:], deepgramTTSSampleRate)
	binary.LittleEndian.PutUint32(out[28:], byteRate)
	binary.LittleEndian.PutUint16(out[32:], blockAlign)
	binary.LittleEndian.PutUint16(out[34:], bitsPerSample)

	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], dataSize)
	copy(out[44:], pcm)

	return out
}
