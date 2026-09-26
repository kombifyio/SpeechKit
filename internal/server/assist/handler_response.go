//go:build linux

package assist

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/internal/server/httpx"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	assistpkg "github.com/kombifyio/SpeechKit/pkg/speechkit/assist"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/localization"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/speaker"
)

type processResponse struct {
	Text                 string                          `json:"text"`
	SpeakText            string                          `json:"speak_text,omitempty"`
	Action               string                          `json:"action"`
	Locale               string                          `json:"locale,omitempty"`
	Shortcut             string                          `json:"shortcut,omitempty"`
	Surface              string                          `json:"surface,omitempty"`
	Kind                 string                          `json:"kind,omitempty"`
	MessageID            localization.MessageID          `json:"message_id,omitempty"`
	ReasonCode           string                          `json:"reason_code,omitempty"`
	Transcript           string                          `json:"transcript,omitempty"`
	AudioBase64          string                          `json:"audio_base64,omitempty"`
	AudioFormat          string                          `json:"audio_format,omitempty"`
	LatencyMs            int64                           `json:"latency_ms"`
	SourceInfo           *sourceMeta                     `json:"source,omitempty"`
	Speakers             *speaker.DiarizationResult      `json:"speakers,omitempty"`
	CustomizationActions []speechkit.CustomizationAction `json:"customization_actions,omitempty"`
}

type selfTestResponse struct {
	Status    string         `json:"status"`
	Text      string         `json:"text,omitempty"`
	Action    string         `json:"action,omitempty"`
	Locale    string         `json:"locale,omitempty"`
	LatencyMs int64          `json:"latency_ms"`
	Details   map[string]any `json:"details,omitempty"`
}

type sourceMeta struct {
	Format     string `json:"format"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
	DurationMs int64  `json:"duration_ms"`
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func writePipelineError(w http.ResponseWriter, err error, latency time.Duration) {
	classification := classifyPipelineError(err)
	classification["latency_ms"] = latency.Milliseconds()
	message := "Assist pipeline failed"
	if stage, _ := classification["stage"].(string); stage != "" {
		message += " during " + stage
	}
	if category, _ := classification["category"].(string); category != "" {
		message += ": " + category
	}
	httpx.WriteErrorWithDetails(w, http.StatusServiceUnavailable, "pipeline_unavailable", message, classification)
}

func classifyPipelineError(err error) map[string]any {
	details := map[string]any{
		"stage":     "pipeline",
		"category":  "runtime_error",
		"retryable": true,
	}
	if err == nil {
		return details
	}
	// No Generator was configured for a request that needed one: the
	// public service reports this with a sentinel rather than message text.
	if errors.Is(err, assistpkg.ErrMissingHandler) {
		details["stage"] = "llm"
		details["category"] = "missing_model"
		details["retryable"] = false
		return details
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "llm failed") || strings.Contains(lower, "no models configured") {
		details["stage"] = "llm"
	}
	if strings.Contains(lower, "invalid configuration") {
		details["stage"] = "llm"
		details["category"] = "provider_config"
		details["retryable"] = false
		return details
	}
	if strings.Contains(lower, "no models configured") {
		details["category"] = "missing_model"
		details["retryable"] = false
		return details
	}
	if strings.Contains(lower, "rate limit") || strings.Contains(lower, "429") {
		details["category"] = "rate_limited"
		return details
	}
	if strings.Contains(lower, "unauthorized") || strings.Contains(lower, "forbidden") || strings.Contains(lower, "401") || strings.Contains(lower, "403") {
		details["category"] = "credentials"
		details["retryable"] = false
		return details
	}
	return details
}
