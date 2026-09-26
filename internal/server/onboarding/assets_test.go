package onboarding

import (
	"strings"
	"testing"
)

func TestTestUIHTMLSmokeTokenSentinelSurvivesRewrite(t *testing.T) {
	// Regression: a naïve strings.ReplaceAll over the whole HTML rewrites
	// every occurrence of the placeholder — including any JS guard that
	// detects "still the placeholder". If the JS guard is collapsed by
	// the same rewrite, the smoke flow silently disables itself even
	// when a real token is configured.
	//
	// Production observed on v0.35.3: meta tag carried the token, but
	// smokeBearerToken() returned "" because the literal guard had been
	// rewritten to `value === "<real-token>"` (true → return "").
	const realToken = "smoke-regression-token-9aFp42xQ-not-a-real-secret"
	body := TestUIHTML(realToken)

	// The token MUST land in the meta tag (otherwise the page can't auth).
	if !strings.Contains(body, `content="`+realToken+`"`) {
		t.Fatalf("rendered meta tag must carry the real token; want content=%q in body", realToken)
	}

	// The guard inside smokeBearerToken() must NOT compare against the
	// real token. The literal placeholder string `__SPEECHKIT_SMOKE_TOKEN__`
	// either survives by runtime concatenation OR has been removed entirely
	// — but a `=== "<real-token>"` comparison means the rewrite collapsed
	// the guard and the smoke flow is dead.
	forbidden := `=== "` + realToken + `"`
	if strings.Contains(body, forbidden) {
		t.Fatalf("smokeBearerToken() guard was rewritten by ReplaceAll into a self-comparison\n"+
			"this disables smoke auth at runtime; the JS guard must build its sentinel by\n"+
			"runtime concatenation so the Go-side rewrite cannot match it\n"+
			"forbidden substring: %q", forbidden)
	}
}
