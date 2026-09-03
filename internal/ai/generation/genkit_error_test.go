package generation

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"testing"
)

// A model server that is not reachable is a transient condition — llama-server
// still loading after a start, or an update restarting it. Classified
// permanent, every pending meeting summary was marked failed at startup.
func TestClassifyGenkitErrorTreatsAnUnreachableServerAsTransient(t *testing.T) {
	model := Model{Provider: "local", Name: "gemma"}

	refused := fmt.Errorf("gemma request: %w", &url.Error{
		Op:  "Post",
		URL: "http://127.0.0.1:8082/v1/chat/completions",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connectex: No connection could be made because the target machine actively refused it.")},
	})
	classified := classifyGenkitError(model, refused)
	if Kind(classified) != ErrorTransient {
		t.Fatalf("typed dial failure kind = %s, want transient (%v)", Kind(classified), classified)
	}
	var typed *Error
	if !errors.As(classified, &typed) || !typed.Retryable {
		t.Fatalf("typed dial failure must be retryable: %+v", classified)
	}

	// A plugin that flattened the chain to text still reads as a connection failure.
	flat := classifyGenkitError(model, errors.New(`Post "http://127.0.0.1:8082/v1/chat/completions": dial tcp 127.0.0.1:8082: connectex: No connection could be made because the target machine actively refused it.`))
	if Kind(flat) != ErrorTransient {
		t.Fatalf("flattened dial failure kind = %s, want transient", Kind(flat))
	}

	// A real rejection of the request stays permanent.
	if Kind(classifyGenkitError(model, errors.New("model rejected the request: invalid schema"))) != ErrorPermanent {
		t.Fatal("a model-side rejection must stay permanent")
	}
	// The existing classes keep their precedence.
	if Kind(classifyGenkitError(model, errors.New("HTTP 429 rate limit exceeded"))) != ErrorQuota {
		t.Fatal("rate limit must still classify as quota")
	}
}
