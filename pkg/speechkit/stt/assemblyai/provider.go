// Package assemblyai adapts the AssemblyAI speech-to-text products to
// [stt.STTProvider]: the synchronous endpoint on sync.assemblyai.com for
// short dictation clips, the async upload-and-poll API on api.assemblyai.com
// for everything else (diarization, speaker identification, redaction), and
// the v3 realtime WebSocket for live dictation and speaker streams. It needs
// an AssemblyAI API key and public https egress; there is no local component.
package assemblyai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/provideropts"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

const (
	assemblyAIBaseURL          = "https://api.assemblyai.com"
	assemblyAISyncBaseURL      = "https://sync.assemblyai.com"
	assemblyAIStreamingBaseURL = "wss://streaming.assemblyai.com"
	// assemblyAIFlagshipModel is Universal-3.5 Pro — AssemblyAI's flagship
	// speech model, shared by the async, sync, and realtime products.
	assemblyAIFlagshipModel    = "universal-3-5-pro"
	assemblyAIStreamingModel   = assemblyAIFlagshipModel
	assemblyAIMaxResponseBytes = 16 << 20
	// Patient dictation turn detection (AssemblyAI "entity dictation /
	// complex instructions"): tolerate mid-sentence pauses instead of
	// splitting a thought after a breath.
	assemblyAIDictationMinTurnSilenceMs = 200
	assemblyAIDictationMaxTurnSilenceMs = 2000
)

// Provider transcribes through AssemblyAI.
type Provider struct {
	APIKey           string
	Models           []string
	StreamingModel   string
	BaseURL          string
	StreamingBaseURL string
	// SyncBaseURL points at the synchronous transcription endpoint
	// (https://sync.assemblyai.com; regional variants sync.us / sync.eu
	// exist). The Sync API returns a finished Universal-3.5 Pro transcript
	// in one request/response (~134 ms p50) for clips up to 120 s / 40 MB —
	// the low-latency dictation path. Empty uses the global endpoint.
	SyncBaseURL string
	// SyncModel is the X-AAI-Model routing header value for sync requests.
	// Empty uses universal-3-5-pro.
	SyncModel string
	// DisableSync forces every transcription through the classic async
	// upload+poll flow, even for clips the Sync API could serve.
	DisableSync bool
	// StreamingLLM, when set, attaches AssemblyAI LLM Gateway to Universal-3.5
	// Pro realtime turns. The formatted transcript still arrives as Turn;
	// LLMGatewayResponse may rewrite the final text for live cleanup.
	StreamingLLM *StreamingLLM
	Validation   netsec.ValidationOptions
	PollInterval time.Duration
	PollTimeout  time.Duration
	client       *http.Client
}

// StreamingLLM is the LLM Gateway payload attached to a realtime
// dictation WebSocket. Model IDs are LLM Gateway catalog names, not STT names.
type StreamingLLM struct {
	Model     string
	Prompt    string
	MaxTokens int
}

// DefaultTurnCleanupPrompt asks the gateway to tidy a single turn
// without summarizing. {{turn}} is substituted by AssemblyAI.
const DefaultTurnCleanupPrompt = "Clean this dictation turn. Fix punctuation and obvious recognition errors. Keep the same language and meaning. Output only the cleaned text.\n\nTranscript: {{turn}}"

// EnableStreamingLLM attaches LLM Gateway cleanup to realtime dictation.
func (p *Provider) EnableStreamingLLM(model, prompt string, maxTokens int) {
	if p == nil {
		return
	}
	model = strings.TrimSpace(model)
	if model == "" {
		model = "qwen3.5-4b-32k-fast"
	}
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		prompt = DefaultTurnCleanupPrompt
	}
	if maxTokens <= 0 {
		maxTokens = 256
	}
	p.StreamingLLM = &StreamingLLM{Model: model, Prompt: prompt, MaxTokens: maxTokens}
}

// New creates an AssemblyAI provider. models is the
// comma-separated model list; empty uses the provider default.
func New(apiKey, models string) *Provider {
	p := &Provider{
		APIKey:           apiKey,
		Models:           parseAssemblyAIModels(models),
		StreamingModel:   assemblyAIStreamingModel,
		BaseURL:          assemblyAIBaseURL,
		SyncBaseURL:      assemblyAISyncBaseURL,
		StreamingBaseURL: assemblyAIStreamingBaseURL,
		PollInterval:     3 * time.Second,
		PollTimeout:      5 * time.Minute,
	}
	p.client = netsec.NewSafeHTTPClient(netsec.ClientOptions{Timeout: 90 * time.Second, DialValidation: &p.Validation})
	return p
}

// Transcribe implements [stt.STTProvider]. Short plain clips go through the
// synchronous endpoint in one request; clips that need async-only features
// (diarization, speaker identification, PII redaction, the medical domain)
// or exceed the sync limits take the upload, create and poll flow, as does
// any clip whose sync attempt failed (that error stays in the returned error
// chain). The language defaults to German unless resolved otherwise, and the
// result Model lists the model fallback chain sent to AssemblyAI.
func (p *Provider) Transcribe(ctx context.Context, audio []byte, opts stt.TranscribeOpts) (*stt.Result, error) {
	start := time.Now()
	wav := stt.EnsureTranscriptionWAV(audio)
	resolved := stt.ResolveTranscribeOptions("assemblyai", assemblyAIProfileID(opts.Model), opts, provideropts.Values{
		provideropts.OptionLanguage:       "de",
		provideropts.OptionVocabularyBias: true,
	}, nil)
	speakerOpts := resolved.Speaker

	// Sync-first: short plain dictation clips go through the synchronous
	// Universal-3.5 Pro endpoint (single request, no upload+poll round
	// trips). Requests that need async-only features — diarization, speaker
	// identification, PII redaction, the medical domain overlay — or that
	// exceed the sync limits keep the classic flow. A sync failure falls
	// back to async so a regional sync outage never breaks dictation.
	var syncErr error
	if p.syncEligible(wav, opts, resolved) {
		result, err := p.transcribeSync(ctx, wav, start, opts, resolved)
		if err == nil {
			return result, nil
		}
		syncErr = err
	}

	uploadURL, err := p.upload(ctx, wav)
	if err != nil {
		return nil, joinSyncErr(err, syncErr)
	}
	transcriptID, err := p.createTranscript(ctx, uploadURL, opts, resolved)
	if err != nil {
		return nil, joinSyncErr(err, syncErr)
	}
	transcript, err := p.pollTranscript(ctx, transcriptID)
	if err != nil {
		return nil, joinSyncErr(err, syncErr)
	}

	lang := stt.FirstNonEmptyTrimmed(transcript.LanguageCode, resolved.Language, "de")
	model := strings.Join(p.modelsForRequest(opts.Model), ",")
	result := &stt.Result{
		Text:       strings.TrimSpace(transcript.Text),
		Language:   lang,
		Duration:   time.Since(start),
		Provider:   p.Name(),
		Model:      model,
		Confidence: transcript.Confidence,
	}
	if speakerOpts.WantsDiarization() {
		result.Speakers = transcript.diarizationResult(p.Name(), model, lang, speakerOpts)
	}
	return result, nil
}

// joinSyncErr keeps a preceding sync-path failure visible (and unwrappable)
// when the async fallback also fails.
func joinSyncErr(asyncErr, syncErr error) error {
	if syncErr == nil {
		return asyncErr
	}
	return fmt.Errorf("%w (sync attempt failed first: %w)", asyncErr, syncErr)
}

// Name returns "assemblyai".
func (p *Provider) Name() string {
	return "assemblyai"
}

// Health issues an authenticated GET on the transcript collection; a
// transport failure or non-200 status is returned as the error.
func (p *Provider) Health(ctx context.Context) error {
	endpoint, err := netsec.BuildEndpoint(stt.FirstNonEmptyTrimmed(p.BaseURL, assemblyAIBaseURL), "v2/transcript", p.Validation)
	if err != nil {
		return fmt.Errorf("assemblyai endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return err
	}
	p.authorize(req)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("assemblyai health: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	body, _ := io.ReadAll(io.LimitReader(resp.Body, stt.MaxResponseBytes))
	if resp.StatusCode != http.StatusOK {
		return netsec.ProviderStatusError("assemblyai health", resp.StatusCode, body)
	}
	return nil
}

func (p *Provider) doJSON(req *http.Request, target any) error {
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	body, err := io.ReadAll(io.LimitReader(resp.Body, assemblyAIMaxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return netsec.ProviderStatusError("assemblyai", resp.StatusCode, body)
	}
	if target == nil {
		return nil
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	return nil
}

func (p *Provider) authorize(req *http.Request) {
	req.Header.Set("Authorization", p.APIKey)
}

func (p *Provider) modelsForRequest(override string) []string {
	models := parseAssemblyAIModels(override)
	if len(models) == 0 {
		models = p.Models
	}
	if len(models) == 0 {
		models = []string{assemblyAIFlagshipModel, "universal-2"}
	}
	return append([]string(nil), models...)
}

func assemblyAIProfileID(model string) string {
	if strings.Contains(strings.ToLower(strings.TrimSpace(model)), "diarization") {
		return "stt.assemblyai.universal-diarization"
	}
	return "stt.assemblyai.universal"
}

func parseAssemblyAIModels(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// Capabilities reports what this provider does beyond plain transcription:
// besides separating voices it attributes them to caller-supplied names or
// roles.
func (*Provider) Capabilities() []speechkit.Capability {
	return append(stt.BaseCapabilities(),
		speechkit.CapabilitySpeakerDiarization,
		speechkit.CapabilitySpeakerAttribution,
		speechkit.CapabilitySpeakerIdentification,
	)
}
