//go:build linux

package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	dataprofile "github.com/kombifyio/SpeechKit/internal/dataprofilev1"
)

const profileModeEnv = "SPEECHKIT_DATA_PROFILE_MODE"

func profileContentPath(r *http.Request) string {
	path := strings.TrimPrefix(r.URL.Path, "/api")
	for _, resource := range []string{"/v1/transcripts", "/v1/wakeword/activations"} {
		if path == resource || strings.HasPrefix(path, resource+"/") {
			return path
		}
	}
	for _, prefix := range []string{"/v1/dictation/", "/v1/voiceagent/", "/v1/assist/", "/v1/tts/", "/v1/audio/", "/v1/device-agent/assist", "/v1/device-agent/tts", "/v1/anonymous/"} {
		if strings.HasPrefix(path, prefix) {
			return path
		}
	}
	return ""
}

func writeProfileDenial(w http.ResponseWriter, r *http.Request, profile dataprofile.Profile, unresolved bool) {
	denial := dataprofile.CapabilityDenial(r.Header.Get("X-Request-ID"), "voice.hosted", "", profile)
	if unresolved {
		denial = dataprofile.UnresolvableDenial(r.Header.Get("X-Request-ID"))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(denial)
}
