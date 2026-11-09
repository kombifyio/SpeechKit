//go:build linux

package middleware

import (
	"context"
	"net/http"

	dataprofile "github.com/kombifyio/SpeechKit/internal/dataprofilev1"
)

// Standalone builds have no hosted profile transport. Explicit enforcement
// and carried profiles require an admission adapter before content can run.
func hasProfileHeaders(*http.Request) bool { return false }

func profileKeysFromEnv() []EdgeKey { return nil }

func (a authRuntime) withDataProfile(w http.ResponseWriter, r *http.Request, _ Identity) (context.Context, bool) {
	ctx := r.Context()
	_, carried := dataprofile.FromContext(ctx)
	if carried || (profileContentPath(r) != "" && a.profileMode != "" && a.profileMode != "off") {
		writeProfileDenial(w, r, dataprofile.Profile{}, true)
		return ctx, false
	}
	return ctx, true
}
