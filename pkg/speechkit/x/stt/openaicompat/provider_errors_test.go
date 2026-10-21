package openaicompat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
)

func TestTranscribeRateLimitIsTypedWithRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		http.Error(w, "slow down", http.StatusTooManyRequests)
	}))
	defer server.Close()

	p := New(Options{Name: "test", BaseURL: server.URL, APIKey: "key", Model: "model"})
	p.Validation = testValidation
	_, err := p.Transcribe(context.Background(), []byte("wav"), stt.TranscribeOpts{})

	var pe *stt.ProviderError
	if !errors.Is(err, stt.ErrRateLimited) || !errors.As(err, &pe) {
		t.Fatalf("err = %v, want typed rate limit error", err)
	}
	if !pe.Retryable || pe.RetryAfter != 7*time.Second {
		t.Fatalf("Retryable=%v RetryAfter=%v, want true/7s", pe.Retryable, pe.RetryAfter)
	}
}
