//go:build linux

// Package assist implements the POST /v1/assist/process handler. It accepts
// either an audio payload (→ STT → Assist service) or a text transcript
// directly, runs the framework's public Assist service, and returns the
// result plus optional TTS audio as base64 in the JSON response.
//
// Host-side tool execution (clipboard, selection, quick-note) is NOT done
// server-side — the server returns an `action: "execute"` signal and the
// calling client performs the action. This keeps the Server-Target safe
// for multi-tenant deployments where host-level side effects would be
// nonsensical.
package assist

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/kombifyio/SpeechKit/internal/server/audio"
	"github.com/kombifyio/SpeechKit/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/internal/store"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// sessionKeyFromRequest derives a stable v0.38.0 multi-turn session
// key from the request's Identity. The key is opaque to the
// service — it just needs to be the same value across the user's
// follow-up turns and unique across users so contexts do not bleed.
// Identity{UserID, OrgID} satisfies both constraints. When no
// Identity is on the context (e.g. auth_mode=none), we fall back to
// the per-process anonymous default which still gives each user
// their own conversation slot.
func sessionKeyFromRequest(r *http.Request) string {
	id := middleware.IdentityFromContext(r.Context())
	if id.OrgID != "" {
		return id.OrgID + "/" + id.UserID
	}
	return id.UserID
}

// Transcriber is the STT surface the handler needs when the caller sends
// audio. If nil, only text-only requests are accepted.
type Transcriber interface {
	Route(ctx context.Context, audio []byte, audioDurationSecs float64, opts stt.TranscribeOpts) (*stt.Result, error)
}

// Processor is the Assist surface the handler needs: the public
// speechkit.AssistService contract. The production implementation is
// *pkg/speechkit/assist.Service; tests supply a fake.
type Processor = speechkit.AssistService

// Options configures a single Handler instance.
type Options struct {
	Processor              Processor
	Transcriber            Transcriber // optional; nil disables audio input
	MaxUploadMB            int
	MaxDecodedAudioSeconds int
	DefaultLocale          string
	Store                  store.Store
	ActiveTemplateIDs      []string
}

// Handler implements the /v1/assist/process HTTP surface.
type Handler struct {
	processor       Processor
	transcriber     Transcriber
	maxBytes        int64
	decodeLimits    audio.DecodeLimits
	defaultLocale   string
	store           store.Store
	activeTemplates []string
}

// New constructs a Handler. processor must be non-nil. Transcriber is
// optional — when omitted, the handler rejects requests that carry audio
// with a 400/missing-transcriber code.
func New(opts Options) (*Handler, error) {
	if opts.Processor == nil {
		return nil, errors.New("assist: processor must not be nil")
	}
	maxBytes := int64(25) << 20
	if opts.MaxUploadMB > 0 {
		maxBytes = int64(opts.MaxUploadMB) << 20
	}
	return &Handler{
		processor:       opts.Processor,
		transcriber:     opts.Transcriber,
		maxBytes:        maxBytes,
		decodeLimits:    audio.DecodeLimits{MaxDecodedAudioSeconds: opts.MaxDecodedAudioSeconds},
		defaultLocale:   strings.TrimSpace(opts.DefaultLocale),
		store:           opts.Store,
		activeTemplates: append([]string(nil), opts.ActiveTemplateIDs...),
	}, nil
}

// Mount registers the handler at /v1/assist/process.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.Handle("/v1/assist/process", h)
	mux.HandleFunc("/v1/assist/self-test", h.ServeSelfTest)
}

func (h *Handler) resolveLocale(requested string) string {
	if trimmed := strings.TrimSpace(requested); trimmed != "" {
		return trimmed
	}
	return h.defaultLocale
}
