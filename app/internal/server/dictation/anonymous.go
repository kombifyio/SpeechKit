//go:build linux

package dictation

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/kombifyio/SpeechKit/app/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/app/internal/server/middleware"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

// MountAnonymous reuses transcription without user customizations, provider
// overrides, speaker identification or transcript persistence.
func (h *Handler) MountAnonymous(mux *http.ServeMux) error {
	public, err := New(Options{Router: h.router, MaxUploadMB: 4, MaxDecodedAudioSeconds: 30})
	if err != nil {
		return err
	}
	mux.HandleFunc("/v1/anonymous/dictation/transcribe", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !middleware.IsAnonymousEdge(middleware.IdentityFromContext(r.Context())) {
			httpx.WriteError(w, http.StatusForbidden, "anonymous_admission_required", "Use public dictation through Gateway.")
			return
		}
		var input struct {
			AudioBase64 string `json:"audio_base64"`
			Format      string `json:"format"`
			Language    string `json:"language"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || input.Format != "wav" || (input.Language != "" && input.Language != "auto") {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_request", "Send WAV audio for automatic multilingual recognition.")
			return
		}
		data, err := base64.StdEncoding.DecodeString(input.AudioBase64)
		if err != nil || len(data) == 0 {
			httpx.WriteError(w, http.StatusBadRequest, "invalid_audio", "Audio must be base64-encoded WAV.")
			return
		}
		public.transcribeBytes(w, r, data, "audio/wav", stt.TranscribeOpts{Language: "auto"})
	})
	return nil
}
