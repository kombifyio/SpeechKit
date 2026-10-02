package netsec

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

func TestRedactURLErrorScrubsCredentialsFromWrappedURLErrors(t *testing.T) {
	transport := &url.Error{
		Op:  "Post",
		URL: "https://user:pw-secret@speech.example.test/v1/speech:recognize?key=AIza-secret&sig=sig-secret",
		Err: errors.New("context deadline exceeded"),
	}
	err := RedactURLError(fmt.Errorf("stt: %w", errors.Join(errors.New("other"), transport)))

	msg := err.Error()
	for _, secret := range []string{"AIza-secret", "sig-secret", "pw-secret", "user:"} {
		if strings.Contains(msg, secret) {
			t.Fatalf("redacted error still contains %q: %s", secret, msg)
		}
	}
	if !strings.Contains(msg, "speech.example.test") {
		t.Fatalf("redacted error lost the host needed for diagnostics: %s", msg)
	}
}
