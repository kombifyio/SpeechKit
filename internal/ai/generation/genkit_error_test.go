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
func TestClassifyGenerateErrorTreatsAnUnreachableServerAsTransient(t *testing.T) {
	model := Model{Provider: "local", Name: "gemma"}

	refused := fmt.Errorf("gemma request: %w", &url.Error{
		Op:  "Post",
		URL: "http://127.0.0.1:8082/v1/chat/completions",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connectex: No connection could be made because the target machine actively refused it.")},
	})
	classified := classifyGenerateError(model, refused)
	if Kind(classified) != ErrorTransient {
		t.Fatalf("typed dial failure kind = %s, want transient (%v)", Kind(classified), classified)
	}
	var typed *Error
	if !errors.As(classified, &typed) || !typed.Retryable {
		t.Fatalf("typed dial failure must be retryable: %+v", classified)
	}

	// A plugin that flattened the chain to text still reads as a connection failure.
	flat := classifyGenerateError(model, errors.New(`Post "http://127.0.0.1:8082/v1/chat/completions": dial tcp 127.0.0.1:8082: connectex: No connection could be made because the target machine actively refused it.`))
	if Kind(flat) != ErrorTransient {
		t.Fatalf("flattened dial failure kind = %s, want transient", Kind(flat))
	}

	// A real rejection of the request stays permanent.
	if Kind(classifyGenerateError(model, errors.New("model rejected the request: invalid schema"))) != ErrorPermanent {
		t.Fatal("a model-side rejection must stay permanent")
	}
	// The existing classes keep their precedence.
	if Kind(classifyGenerateError(model, errors.New("HTTP 429 rate limit exceeded"))) != ErrorQuota {
		t.Fatal("rate limit must still classify as quota")
	}
}

// A provider the user is signed out of must classify as authentication, not
// permanent: the meeting summary processor keeps an authentication failure
// "delayed" and re-runs it, while permanent marks the batch failed for good.
// The 2026-09-15 meeting lost its rollups this way — Foundry answered "not
// signed in" and no keyword matched.
func TestClassifyGenerateErrorTreatsSignedOutProvidersAsAuthentication(t *testing.T) {
	model := Model{Provider: "foundry", Name: "gpt-5.6-luna"}

	signedOut := errors.New("gpt-5.6-luna: bearer token: microsoft foundry: not signed in — sign in on the Microsoft Foundry card in Settings")
	if Kind(classifyGenerateError(model, signedOut)) != ErrorAuthentication {
		t.Fatalf("signed-out provider kind = %s, want authentication", Kind(classifyGenerateError(model, signedOut)))
	}

	for _, message := range []string{
		"unauthorized",
		"invalid api key",
		"azure: token expired",
		"github copilot: please sign-in again",
		"request failed with http status 403",
	} {
		if Kind(classifyGenerateError(model, errors.New(message))) != ErrorAuthentication {
			t.Fatalf("%q kind = %s, want authentication", message, Kind(classifyGenerateError(model, errors.New(message))))
		}
	}

	// Digits that only look like status codes keep their own class.
	sized := classifyGenerateError(model, errors.New("model rejected the request: prompt of 403 tokens is malformed"))
	if Kind(sized) != ErrorPermanent {
		t.Fatalf("a stray 403 in prose kind = %s, want permanent", Kind(sized))
	}
	// Precedence: a context-limit message wins over the credential wording it carries.
	limit := classifyGenerateError(model, errors.New("api key ok but context length exceeded"))
	if Kind(limit) != ErrorContextLimit {
		t.Fatalf("context limit kind = %s, want context_limit", Kind(limit))
	}
}
