//go:build linux

package core

import (
	"errors"
	"os"
	"strings"

	vsserver "github.com/kombifyio/SpeechKit/app/internal/server/voiceagent"
)

// The standalone server supports generic bearer-authenticated usage reporting.
func buildVoiceUsageReporter() (vsserver.UsageReporter, error) {
	endpoint := strings.TrimSpace(os.Getenv("KOMBIFY_USAGE_ENDPOINT"))
	switch strings.ToLower(strings.TrimSpace(os.Getenv("KOMBIFY_USAGE_AUTH_MODE"))) {
	case "", "bearer":
		if endpoint == "" {
			return nil, nil
		}
		return vsserver.NewHTTPUsageReporter(endpoint), nil
	case "service":
		return nil, errors.New("voiceagent usage service mode is unavailable in this server")
	default:
		return nil, errors.New("voiceagent usage auth mode is invalid")
	}
}
