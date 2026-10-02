package openaicompat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/coder/websocket"

	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/netsec"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/voiceagent/live"
)

// OpenAILiveTranscribeModel is OpenAI's streaming speech-to-text model
// (released 2026-07-28). It is served over a GA Realtime transcription
// session, not the /v1/audio/transcriptions upload the batch path uses.
const OpenAILiveTranscribeModel = "gpt-live-transcribe"

// ErrDictationStreamUnsupported is returned by [Provider.StartDictationStream]
// for every OpenAI-compatible endpoint except OpenAI itself: Groq, Ollama and
// whisper-server speak the batch upload API only. Callers errors.Is it to
// fall back to batch transcription.
var ErrDictationStreamUnsupported = errors.New("openaicompat: native dictation streaming is served only by the openai provider")

// Wire protocol, derived from the openai-python SDK (src/openai, 2026-09-30):
//
//   - URL: resources/realtime/realtime.py _prepare_url/_connect_ws append
//     "/realtime" to the API base and switch the scheme to wss; a
//     transcription-only session is selected with intent=transcription.
//   - session.update: types/realtime/session_update_event_param.py with a
//     realtime_transcription_session_create_request_param.py session
//     (type "transcription") whose audio.input
//     (realtime_transcription_session_audio_input_param.py) carries format
//     (realtime_audio_formats_param.py: audio/pcm, rate 24000 only),
//     transcription (audio_transcription_param.py: model, language, prompt,
//     keywords) and turn_detection
//     (realtime_transcription_session_audio_input_turn_detection_param.py).
//   - client events: input_audio_buffer_append_event_param.py (base64 audio)
//     and input_audio_buffer_commit_event_param.py.
//   - server events: conversation_item_input_audio_transcription_delta_event.py,
//     ..._completed_event.py, ..._failed_event.py,
//     input_audio_buffer_committed_event.py, realtime_error_event.py.
//
// GA Realtime takes no OpenAI-Beta header; only Authorization is sent.
const (
	openAILiveTranscribeRate = 24000
	// finalizeUpdateEventID and finalizeCommitEventID tag the two client
	// events Finalize sends so an error event (whose error.event_id echoes
	// the client id) can be attributed to them.
	finalizeUpdateEventID = "speechkit_finalize_update"
	finalizeCommitEventID = "speechkit_finalize_commit"
)

// SupportsDictationStream implements [speechkit.DictationStreamAvailability]:
// the adapter also serves Groq, Ollama and self-hosted servers, and only the
// OpenAI API streams live dictation.
func (p *Provider) SupportsDictationStream() bool { return p.name == "openai" }

// StartDictationStream implements [speechkit.DictationStreamProvider] with
// gpt-live-transcribe over a GA Realtime transcription session. Only the
// "openai" provider serves it; every other name fails with
// [ErrDictationStreamUnsupported].
//
// The model is opts.Model or Provider.Model when either names a
// gpt-live-transcribe model, else [OpenAILiveTranscribeModel]. Server VAD
// cuts the audio into turns (silence_duration_ms from opts.EndpointingMs when
// set); each turn's deltas surface as drafts when opts.InterimResults is on
// and its completed transcript as the final. opts.Keyterms ride the native
// keywords field, opts.PromptHint the prompt, and opts.Language its ISO-639-1
// primary subtag unless it is multilanguage.
//
// The API accepts 24 kHz PCM only: 24 kHz mono linear16 passes through and
// 16 kHz mono linear16 (SpeechKit's mic rate) is upsampled per chunk with
// [live.UpsampleMicPCM16Mono]; anything else fails with
// [speechkit.ErrUnsupportedAudioFormat].
//
// Finalize turns server VAD off and commits the trailing buffer; Receive
// then drains every committed turn and returns io.EOF, matching SpeechKit's
// one-provider-stream-per-segment model.
func (p *Provider) StartDictationStream(ctx context.Context, opts speechkit.DictationStreamOptions, format speaker.AudioFormat) (speechkit.DictationStream, error) {
	if !p.SupportsDictationStream() {
		return nil, fmt.Errorf("%s: %w", p.name, ErrDictationStreamUnsupported)
	}
	format = format.Normalized()
	if format.Channels != 1 ||
		(format.Encoding != speaker.AudioEncodingLinear16 && format.Encoding != speaker.AudioEncodingPCM16) ||
		(format.SampleRateHz != 16000 && format.SampleRateHz != openAILiveTranscribeRate) {
		return nil, fmt.Errorf(
			"openai live transcription requires mono 16-bit PCM at 16 or 24 kHz (the Realtime API accepts 24 kHz audio/pcm only); got %s %d Hz %d channels: %w",
			format.Encoding, format.SampleRateHz, format.Channels, speechkit.ErrUnsupportedAudioFormat)
	}
	model := openAILiveTranscribeModel(opts.Model, p.Model)
	endpoint, err := p.liveTranscribeEndpoint()
	if err != nil {
		return nil, err
	}
	headers, err := p.liveTranscribeHeaders(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	conn, resp, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPClient: p.client,
		HTTPHeader: headers,
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("openai live transcription dial: %w", err)
	}
	// A realtime turn can exceed coder/websocket's 32 KiB default read limit
	// once the completed transcript carries usage and language fields.
	conn.SetReadLimit(1 << 20)
	stream := &openAILiveTranscribeStream{
		conn:      conn,
		provider:  p.Name(),
		model:     model,
		language:  liveTranscribeEventLanguage(opts.Language),
		sessionID: opts.SessionID,
		interim:   opts.InterimResults,
		upsample:  format.SampleRateHz != openAILiveTranscribeRate,
		segments:  map[string]uint64{},
		drafts:    map[string]*strings.Builder{},
		pending:   map[string]struct{}{},
	}
	if err := stream.writeJSON(ctx, liveTranscribeSessionUpdate(model, opts)); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "session update failed")
		return nil, fmt.Errorf("openai live transcription session.update: %w", err)
	}
	return stream, nil
}

// liveTranscribeEndpoint resolves the validated GA Realtime WebSocket URL
// from BaseURL: {base}/v1/realtime?intent=transcription over wss.
func (p *Provider) liveTranscribeEndpoint() (string, error) {
	endpoint, err := netsec.BuildEndpoint(p.BaseURL, "v1/realtime", p.Validation)
	if err != nil {
		return "", fmt.Errorf("%s realtime endpoint: %w", p.name, err)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("%s realtime endpoint parse: %w", p.name, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("%s realtime endpoint: unsupported scheme %q", p.name, u.Scheme)
	}
	q := u.Query()
	q.Set("intent", "transcription")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// liveTranscribeHeaders reuses the batch path's credential logic (bearer
// token source over static key) for the WebSocket handshake.
func (p *Provider) liveTranscribeHeaders(ctx context.Context, endpoint string) (http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("%s realtime request: %w", p.name, err)
	}
	if err := p.authorize(ctx, req); err != nil {
		return nil, err
	}
	return req.Header, nil
}

// openAILiveTranscribeModel keeps a requested or configured model only when
// it is a gpt-live-transcribe id (dated snapshots included); the batch
// default gpt-transcribe is not served by the streaming session.
func openAILiveTranscribeModel(candidates ...string) string {
	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if strings.HasPrefix(strings.ToLower(trimmed), OpenAILiveTranscribeModel) {
			return trimmed
		}
	}
	return OpenAILiveTranscribeModel
}

func liveTranscribeSessionUpdate(model string, opts speechkit.DictationStreamOptions) map[string]any {
	transcription := map[string]any{"model": model}
	if language := liveTranscribeAPILanguage(opts.Language); language != "" {
		transcription["language"] = language
	}
	if hint := strings.TrimSpace(opts.PromptHint); hint != "" {
		transcription["prompt"] = hint
	}
	keywords := make([]string, 0, len(opts.Keyterms))
	for _, term := range opts.Keyterms {
		if term = strings.TrimSpace(term); term != "" {
			keywords = append(keywords, term)
		}
	}
	if len(keywords) > 0 {
		transcription["keywords"] = keywords
	}
	turnDetection := map[string]any{"type": "server_vad"}
	if opts.EndpointingMs > 0 {
		turnDetection["silence_duration_ms"] = opts.EndpointingMs
	}
	return map[string]any{
		"type": "session.update",
		"session": map[string]any{
			"type": "transcription",
			"audio": map[string]any{
				"input": map[string]any{
					"format":         map[string]any{"type": "audio/pcm", "rate": openAILiveTranscribeRate},
					"transcription":  transcription,
					"turn_detection": turnDetection,
				},
			},
		},
	}
}

// liveTranscribeAPILanguage maps a BCP-47 locale to the ISO-639-1 code the
// transcription config takes ("de-DE" -> "de", "zh-Hans" -> "zh"), or "" to
// let the model detect it.
func liveTranscribeAPILanguage(language string) string {
	if stt.IsMultilanguage(language) {
		return ""
	}
	primary, _, _ := strings.Cut(strings.ReplaceAll(strings.TrimSpace(language), "_", "-"), "-")
	return strings.ToLower(primary)
}

// liveTranscribeEventLanguage is the locale stamped on emitted events: the
// caller's BCP-47 value unchanged, or "" when multilanguage.
func liveTranscribeEventLanguage(language string) string {
	if stt.IsMultilanguage(language) {
		return ""
	}
	return strings.TrimSpace(language)
}

// openAILiveTranscribeStream adapts a GA Realtime transcription session to
// speechkit.DictationStream. Receive owns segments, drafts, pending and the
// finalize bookkeeping; only finalizing crosses goroutines.
type openAILiveTranscribeStream struct {
	conn      *websocket.Conn
	provider  string
	model     string
	language  string
	sessionID uint64
	interim   bool
	upsample  bool

	sequence   atomic.Int64
	finalizing atomic.Bool
	closeOnce  atomic.Bool

	segments       map[string]uint64
	drafts         map[string]*strings.Builder
	pending        map[string]struct{}
	sessionUpdates int
	vadOff         bool
	commitAcked    bool
	done           bool
}

func (s *openAILiveTranscribeStream) writeJSON(ctx context.Context, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.conn.Write(ctx, websocket.MessageText, body)
}

func (s *openAILiveTranscribeStream) SendPCM(ctx context.Context, pcm []byte) error {
	if len(pcm) == 0 {
		return nil
	}
	if s.upsample {
		pcm = live.UpsampleMicPCM16Mono(pcm)
	}
	return s.writeJSON(ctx, map[string]any{
		"type":  "input_audio_buffer.append",
		"audio": base64.StdEncoding.EncodeToString(pcm),
	})
}

// Finalize turns server VAD off — so no automatic commit can race the
// client's — and commits whatever audio is still buffered. Receive then
// returns the remaining finals followed by io.EOF.
func (s *openAILiveTranscribeStream) Finalize(ctx context.Context) error {
	if s.finalizing.Swap(true) {
		return nil
	}
	if err := s.writeJSON(ctx, map[string]any{
		"type":     "session.update",
		"event_id": finalizeUpdateEventID,
		"session": map[string]any{
			"type":  "transcription",
			"audio": map[string]any{"input": map[string]any{"turn_detection": nil}},
		},
	}); err != nil {
		return err
	}
	return s.writeJSON(ctx, map[string]any{
		"type":     "input_audio_buffer.commit",
		"event_id": finalizeCommitEventID,
	})
}

type openAILiveTranscribeEvent struct {
	Type       string `json:"type"`
	ItemID     string `json:"item_id"`
	Delta      string `json:"delta"`
	Transcript string `json:"transcript"`
	Languages  []struct {
		Code string `json:"code"`
	} `json:"languages"`
	Error *struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Message string `json:"message"`
		EventID string `json:"event_id"`
	} `json:"error"`
}

func (s *openAILiveTranscribeStream) Receive(ctx context.Context) (speechkit.DictationStreamEvent, error) {
	for {
		if s.done {
			return speechkit.DictationStreamEvent{}, io.EOF
		}
		typ, payload, err := s.conn.Read(ctx)
		if err != nil {
			if stt.IsWebSocketClose(err) {
				return speechkit.DictationStreamEvent{}, io.EOF
			}
			return speechkit.DictationStreamEvent{}, err
		}
		if typ != websocket.MessageText {
			continue
		}
		var event openAILiveTranscribeEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			return speechkit.DictationStreamEvent{}, fmt.Errorf("openai live transcription parse: %w", err)
		}
		out, emit, err := s.handle(event)
		if err != nil {
			return speechkit.DictationStreamEvent{}, err
		}
		if s.commitAcked && len(s.pending) == 0 {
			s.done = true
		}
		if emit {
			return out, nil
		}
	}
}

// handle folds one server event into the stream state and reports whether it
// yields a dictation event. Transcript text never reaches an error or log.
func (s *openAILiveTranscribeStream) handle(event openAILiveTranscribeEvent) (speechkit.DictationStreamEvent, bool, error) {
	finalizing := s.finalizing.Load()
	switch event.Type {
	case "session.updated":
		s.sessionUpdates++
		if finalizing && s.sessionUpdates >= 2 {
			s.vadOff = true
		}
	case "input_audio_buffer.committed":
		if event.ItemID != "" {
			s.segment(event.ItemID)
			s.pending[event.ItemID] = struct{}{}
		}
		if finalizing && s.vadOff {
			s.commitAcked = true
		}
	case "conversation.item.input_audio_transcription.delta":
		if !s.interim || event.ItemID == "" || event.Delta == "" {
			break
		}
		draft := s.drafts[event.ItemID]
		if draft == nil {
			draft = &strings.Builder{}
			s.drafts[event.ItemID] = draft
		}
		draft.WriteString(event.Delta)
		text := strings.TrimSpace(draft.String())
		if text == "" {
			break
		}
		return s.event(event.ItemID, text, false, ""), true, nil
	case "conversation.item.input_audio_transcription.completed":
		delete(s.pending, event.ItemID)
		delete(s.drafts, event.ItemID)
		text := strings.TrimSpace(event.Transcript)
		if text == "" {
			break
		}
		detected := ""
		if len(event.Languages) > 0 {
			detected = strings.TrimSpace(event.Languages[0].Code)
		}
		return s.event(event.ItemID, text, true, detected), true, nil
	case "conversation.item.input_audio_transcription.failed":
		delete(s.pending, event.ItemID)
		delete(s.drafts, event.ItemID)
		code := ""
		if event.Error != nil {
			code = stt.FirstNonEmptyTrimmed(event.Error.Code, event.Error.Type)
		}
		return speechkit.DictationStreamEvent{}, false, fmt.Errorf("openai live transcription failed for item %s: %s", event.ItemID, code)
	case "error":
		if event.Error == nil {
			return speechkit.DictationStreamEvent{}, false, errors.New("openai live transcription error")
		}
		switch event.Error.EventID {
		case finalizeUpdateEventID:
			// VAD could not be switched off; fall back to taking the next
			// commit as the finalize acknowledgement.
			s.vadOff = true
			return speechkit.DictationStreamEvent{}, false, nil
		case finalizeCommitEventID:
			// Nothing left to commit (server VAD already took the buffer).
			s.commitAcked = true
			return speechkit.DictationStreamEvent{}, false, nil
		}
		return speechkit.DictationStreamEvent{}, false, fmt.Errorf("openai live transcription error: %s %s: %s",
			event.Error.Type, event.Error.Code, event.Error.Message)
	}
	// session.created, speech_started/stopped, conversation.item.* and
	// future event types carry no transcript.
	return speechkit.DictationStreamEvent{}, false, nil
}

// segment returns the per-turn SegmentID for a Realtime item, numbering items
// in the order they first appear so drafts and the final share one id.
func (s *openAILiveTranscribeStream) segment(itemID string) uint64 {
	if id, ok := s.segments[itemID]; ok {
		return id
	}
	id := uint64(len(s.segments)) + 1
	s.segments[itemID] = id
	return id
}

func (s *openAILiveTranscribeStream) event(itemID, text string, final bool, detectedLanguage string) speechkit.DictationStreamEvent {
	return speechkit.DictationStreamEvent{
		Sequence:       s.sequence.Add(1),
		SessionID:      s.sessionID,
		SegmentID:      s.segment(itemID),
		ProviderItemID: "openai:" + itemID,
		Text:           text,
		IsFinal:        final,
		Language:       stt.FirstNonEmptyTrimmed(s.language, detectedLanguage),
		Provider:       s.provider,
		Model:          s.model,
	}
}

func (s *openAILiveTranscribeStream) Close() error {
	if s.closeOnce.Swap(true) {
		return nil
	}
	return s.conn.Close(websocket.StatusNormalClosure, "dictation stream close")
}
