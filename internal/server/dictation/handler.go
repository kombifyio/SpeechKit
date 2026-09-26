//go:build linux

// Package dictation implements the POST /v1/dictation/transcribe handler.
// It is a thin adapter around the Framework's STT router: accept audio bytes
// and metadata, normalize to canonical PCM via internal/server/audio, and
// delegate to the router. The handler has no knowledge of which provider
// serves the request; that's the router's job.
package dictation

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/server/audio"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// Transcriber is the minimal surface the handler needs from an STT router.
// The production implementation is `internal/router.Router`; tests provide
// a fake.
type Transcriber interface {
	Route(ctx context.Context, audio []byte, audioDurationSecs float64, opts stt.TranscribeOpts) (*stt.Result, error)
}

// Options configures a single handler instance.
type Options struct {
	Router                 Transcriber
	MaxUploadMB            int    // request body ceiling; 0 disables the limit (discouraged)
	MaxDecodedAudioSeconds int    // decoded PCM duration ceiling; 0 disables the decode-duration cap
	DefaultPrompt          string // applied when the request does not provide a prompt
	Store                  store.Store
	ActiveTemplateIDs      []string
	// DefaultProviderProfileID is the server's configured Dictation primary
	// (ModelSelection.Dictate.PrimaryProfileID). It is the lowest-precedence
	// provider preference: explicit request override → edge-injected user
	// preference → this default → the router's configured fallback order.
	DefaultProviderProfileID string
}

// Handler implements the dictation HTTP surface.
type Handler struct {
	router           Transcriber
	maxBytes         int64
	decodeLimits     audio.DecodeLimits
	defaultPrompt    string
	store            store.Store
	activeTemplates  []string
	defaultProfileID string
}

// New constructs a Handler. The router must be non-nil; a zero maxBytes
// defaults to 25 MB to match the documented API contract.
func New(opts Options) (*Handler, error) {
	if opts.Router == nil {
		return nil, errors.New("dictation: router must not be nil")
	}
	maxBytes := int64(25) << 20
	if opts.MaxUploadMB > 0 {
		maxBytes = int64(opts.MaxUploadMB) << 20
	}
	return &Handler{
		router:           opts.Router,
		maxBytes:         maxBytes,
		decodeLimits:     audio.DecodeLimits{MaxDecodedAudioSeconds: opts.MaxDecodedAudioSeconds},
		defaultPrompt:    strings.TrimSpace(opts.DefaultPrompt),
		store:            opts.Store,
		activeTemplates:  append([]string(nil), opts.ActiveTemplateIDs...),
		defaultProfileID: strings.TrimSpace(opts.DefaultProviderProfileID),
	}, nil
}

// Mount registers the handler on the given mux at /v1/dictation/transcribe.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.Handle("/v1/dictation/transcribe", h)
}

// response body shape — kept stable across versions so API consumers and
// future OSS integrators can pin to this contract.
type transcribeResponse struct {
	Text                 string                          `json:"text"`
	Language             string                          `json:"language,omitempty"`
	DurationMs           int64                           `json:"duration_ms"`
	LatencyMs            int64                           `json:"latency_ms"`
	Provider             string                          `json:"provider,omitempty"`
	Model                string                          `json:"model,omitempty"`
	Confidence           float64                         `json:"confidence,omitempty"`
	SourceInfo           *sourceMeta                     `json:"source,omitempty"`
	Speakers             *speaker.DiarizationResult      `json:"speakers,omitempty"`
	CustomizationActions []speechkit.CustomizationAction `json:"customization_actions,omitempty"`
}

type sourceMeta struct {
	Format     string `json:"format"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
	DurationMs int64  `json:"duration_ms"`
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
