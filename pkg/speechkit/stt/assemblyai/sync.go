package assemblyai

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
)

// AssemblyAI Sync Speech-to-Text: POST /transcribe on sync.assemblyai.com
// returns a finished Universal-3.5 Pro transcript in a single
// request/response (~134 ms p50) for clips up to 120 s / 40 MB — the
// low-latency dictation path. Unlike the async API it accepts `prompt` and
// `keyterms_prompt` together plus `conversation_context` (preceding dialogue
// turns), but it has NO diarization, PII redaction, or speech understanding;
// those requests stay on the async upload+poll flow.
const (
	// assemblyAISyncMaxBytes is the documented request size cap (40 MB).
	assemblyAISyncMaxBytes = 40 << 20
	// assemblyAISyncMaxDuration is the documented clip duration cap (120 s).
	assemblyAISyncMaxDuration = 120 * time.Second
	// assemblyAISyncMinDuration is the documented clip minimum (80 ms).
	assemblyAISyncMinDuration = 80 * time.Millisecond
	// assemblyAISyncMaxPromptChars caps config.prompt (docs: 4096 chars).
	assemblyAISyncMaxPromptChars = 4096
	// assemblyAISyncMaxKeytermsChars caps the total keyterms_prompt payload
	// (docs: 2048 chars across all terms).
	assemblyAISyncMaxKeytermsChars = 2048
	// assemblyAISyncMaxContextTurns / Chars cap conversation_context
	// (docs: 100 turns, 4096 chars total; provider trims oldest first — we
	// pre-trim to keep requests deterministic).
	assemblyAISyncMaxContextTurns = 100
	assemblyAISyncMaxContextChars = 4096
)

type assemblyAISyncConfig struct {
	Prompt              string   `json:"prompt,omitempty"`
	KeytermsPrompt      []string `json:"keyterms_prompt,omitempty"`
	ConversationContext []string `json:"conversation_context,omitempty"`
	LanguageCode        string   `json:"language_code,omitempty"`
	Timestamps          bool     `json:"timestamps,omitempty"`
}

type assemblyAISyncResponse struct {
	Text            string               `json:"text"`
	Words           []assemblyAISyncWord `json:"words"`
	Confidence      float64              `json:"confidence"`
	AudioDurationMs int64                `json:"audio_duration_ms"`
	SessionID       string               `json:"session_id"`
}

type assemblyAISyncWord struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	Start      int64   `json:"start"`
	End        int64   `json:"end"`
}

// syncEligible reports whether this request can be served by the Sync API.
func (p *Provider) syncEligible(wav []byte, opts stt.TranscribeOpts, resolved stt.ResolvedTranscribeOptions) bool {
	if p.DisableSync {
		return false
	}
	if len(wav) == 0 || len(wav) > assemblyAISyncMaxBytes {
		return false
	}
	// Async-only features.
	if resolved.Speaker.WantsDiarization() || resolved.Speaker.WantsIdentification() {
		return false
	}
	if resolved.PrivacyRedaction || resolved.MedicalDomain || resolved.VoiceFocus {
		return false
	}
	// Language auto-detection is async-only: the Sync API has no
	// language_detection knob and defaults unset language_code to English,
	// which would silently change auto-detect semantics by clip length.
	if language := resolved.APILanguage(); language == "" || strings.EqualFold(language, "auto") {
		return false
	}
	// A caller pinning explicit async model overrides (e.g. a
	// universal-2-only deployment) keeps the async flow; sync always routes
	// to the flagship model via X-AAI-Model.
	if models := p.modelsForRequest(opts.Model); len(models) > 0 && !assemblyAIModelsIncludeFlagship(models) {
		return false
	}
	duration, ok := wavDuration(wav)
	if !ok {
		return false
	}
	return duration >= assemblyAISyncMinDuration && duration <= assemblyAISyncMaxDuration
}

func assemblyAIModelsIncludeFlagship(models []string) bool {
	for _, model := range models {
		if strings.EqualFold(strings.TrimSpace(model), assemblyAIFlagshipModel) {
			return true
		}
	}
	return false
}

func (p *Provider) syncModelHeader() string {
	return stt.FirstNonEmptyTrimmed(p.SyncModel, assemblyAIFlagshipModel)
}

// transcribeSync performs the single-request sync transcription.
func (p *Provider) transcribeSync(ctx context.Context, wav []byte, start time.Time, opts stt.TranscribeOpts, resolved stt.ResolvedTranscribeOptions) (*stt.Result, error) {
	endpoint, err := netsec.BuildEndpoint(stt.FirstNonEmptyTrimmed(p.SyncBaseURL, assemblyAISyncBaseURL), "transcribe", p.Validation)
	if err != nil {
		return nil, fmt.Errorf("assemblyai sync endpoint: %w", err)
	}

	cfg := assemblyAISyncConfig{
		Timestamps: true,
	}
	if prompt := stt.FirstNonEmptyTrimmed(resolved.ContextPrompt, resolved.Prompt); prompt != "" {
		cfg.Prompt = truncateRunes(prompt, assemblyAISyncMaxPromptChars)
	}
	if resolved.UseVocabularyKeyterms && len(resolved.Keyterms) > 0 {
		cfg.KeytermsPrompt = capKeytermsChars(resolved.Keyterms, assemblyAISyncMaxKeytermsChars)
	}
	if len(opts.ConversationContext) > 0 {
		cfg.ConversationContext = capConversationContext(opts.ConversationContext,
			assemblyAISyncMaxContextTurns, assemblyAISyncMaxContextChars)
	}
	// The Sync API ignores language_code when a custom prompt is set (the
	// prompt should then describe the language); sending it anyway is
	// harmless and keeps the no-prompt path correctly steered.
	if language := resolved.APILanguage(); language != "" && !strings.EqualFold(language, "auto") {
		cfg.LanguageCode = language
	}

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	audioHeader := textproto.MIMEHeader{}
	audioHeader.Set("Content-Disposition", `form-data; name="audio"; filename="audio.wav"`)
	audioHeader.Set("Content-Type", "audio/wav")
	audioPart, err := mw.CreatePart(audioHeader)
	if err != nil {
		return nil, fmt.Errorf("assemblyai sync audio part: %w", err)
	}
	if _, err := audioPart.Write(wav); err != nil {
		return nil, fmt.Errorf("assemblyai sync audio write: %w", err)
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("assemblyai sync config marshal: %w", err)
	}
	if err := mw.WriteField("config", string(cfgJSON)); err != nil {
		return nil, fmt.Errorf("assemblyai sync config part: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("assemblyai sync multipart close: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if err != nil {
		return nil, fmt.Errorf("assemblyai sync request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	req.Header.Set("X-AAI-Model", p.syncModelHeader())
	req.Header.Set("Content-Type", mw.FormDataContentType())

	var response assemblyAISyncResponse
	if err := p.doJSON(req, &response); err != nil {
		return nil, fmt.Errorf("assemblyai sync transcribe: %w", err)
	}

	result := &stt.Result{
		Text:       strings.TrimSpace(response.Text),
		Language:   stt.FirstNonEmptyTrimmed(cfg.LanguageCode, resolved.Language, "de"),
		Duration:   time.Since(start),
		Provider:   p.Name(),
		Model:      p.syncModelHeader(),
		Confidence: response.Confidence,
	}
	if len(response.Words) > 0 {
		result.Words = make([]stt.WordConfidence, 0, len(response.Words))
		for _, word := range response.Words {
			result.Words = append(result.Words, stt.WordConfidence{
				Text:       word.Text,
				Confidence: word.Confidence,
				StartMs:    word.Start,
				EndMs:      word.End,
			})
		}
	}
	return result, nil
}

// Warm pre-establishes the HTTPS connection to the Sync API (DNS, TCP, TLS)
// so the subsequent /transcribe request skips connection setup. Hosts should
// call it when recording starts; the endpoint is unauthenticated and safe to
// call repeatedly, and idle connections are evicted after seconds to minutes,
// so warm close to the transcription rather than at startup.
func (p *Provider) Warm(ctx context.Context) error {
	if p.DisableSync {
		return nil
	}
	endpoint, err := netsec.BuildEndpoint(stt.FirstNonEmptyTrimmed(p.SyncBaseURL, assemblyAISyncBaseURL), "warm", p.Validation)
	if err != nil {
		return fmt.Errorf("assemblyai warm endpoint: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return err
	}
	req.Header.Set("X-AAI-Model", p.syncModelHeader())
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("assemblyai warm: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("assemblyai warm: unexpected status %d", resp.StatusCode)
	}
	return nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

// capKeytermsChars keeps keyterms in order until the documented total
// character budget is exhausted.
func capKeytermsChars(terms []string, maxTotalChars int) []string {
	out := make([]string, 0, len(terms))
	total := 0
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		total += len([]rune(term))
		if total > maxTotalChars {
			break
		}
		out = append(out, term)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// capConversationContext keeps the MOST RECENT turns within the turn and
// character budgets (the provider also trims oldest-first; pre-trimming keeps
// what we send deterministic).
func capConversationContext(turns []string, maxTurns, maxTotalChars int) []string {
	cleaned := make([]string, 0, len(turns))
	for _, turn := range turns {
		if turn = strings.TrimSpace(turn); turn != "" {
			cleaned = append(cleaned, turn)
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	if maxTurns > 0 && len(cleaned) > maxTurns {
		cleaned = cleaned[len(cleaned)-maxTurns:]
	}
	if maxTotalChars > 0 {
		total := 0
		start := len(cleaned)
		for i := len(cleaned) - 1; i >= 0; i-- {
			total += len([]rune(cleaned[i]))
			if total > maxTotalChars {
				break
			}
			start = i
		}
		cleaned = cleaned[start:]
	}
	if len(cleaned) == 0 {
		return nil
	}
	return append([]string(nil), cleaned...)
}

// wavDuration derives the clip duration from a PCM WAV header by walking the
// RIFF chunks (fmt byte rate + data size). Returns ok=false for anything that
// is not a parseable PCM WAV.
func wavDuration(wav []byte) (time.Duration, bool) {
	if len(wav) < 44 || string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
		return 0, false
	}
	var byteRate uint32
	var dataSize uint32
	offset := 12
	for offset+8 <= len(wav) {
		chunkID := string(wav[offset : offset+4])
		chunkSize := binary.LittleEndian.Uint32(wav[offset+4 : offset+8])
		bodyStart := offset + 8
		switch chunkID {
		case "fmt ":
			if bodyStart+16 > len(wav) {
				return 0, false
			}
			byteRate = binary.LittleEndian.Uint32(wav[bodyStart+8 : bodyStart+12])
		case "data":
			dataSize = chunkSize
			remaining := len(wav) - bodyStart
			if remaining >= 0 && uint64(dataSize) > uint64(remaining) {
				// Header claims more data than present; trust the bytes.
				dataSize = uint32(remaining) // #nosec G115 -- remaining is bounded by len(wav) which fits uint32 for sync-eligible clips.
			}
		}
		if byteRate > 0 && dataSize > 0 {
			break
		}
		// Chunks are word-aligned.
		advance := int(chunkSize)
		if advance%2 == 1 {
			advance++
		}
		if advance < 0 || bodyStart+advance <= offset {
			return 0, false
		}
		offset = bodyStart + advance
	}
	if byteRate == 0 || dataSize == 0 {
		return 0, false
	}
	seconds := float64(dataSize) / float64(byteRate)
	return time.Duration(seconds * float64(time.Second)), true
}
