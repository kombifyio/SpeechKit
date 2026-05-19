package onboarding

import (
	_ "embed"
	"html"
	"strings"
)

//go:embed assets/testui.html
var testUIHTML string

//go:embed assets/setup.html
var setupUIHTML string

// smokeTokenPlaceholder is the literal sentinel string in testui.html that
// TestUIHTML replaces with the operator-configured public demo token. The
// placeholder lives inside a `<meta>` tag (and a JS init guard) so the
// rendered page can fetch /api/v1/* with Authorization on behalf of an
// anonymous browser visitor. Empty token => no Authorization header is
// added by the page; visitors see 401 on the API tiles, but the smoke
// page itself still renders.
const smokeTokenPlaceholder = "__SPEECHKIT_SMOKE_TOKEN__"

// TestUIHTML returns the smoke / live-runtime onboarding page rendered for
// a single request. `smokeToken` is embedded into the HTML so the page's
// JS can authenticate /api/v1/* fetches from the browser. Pass "" when no
// public demo token is configured.
func TestUIHTML(smokeToken string) string {
	return strings.ReplaceAll(testUIHTML, smokeTokenPlaceholder, html.EscapeString(strings.TrimSpace(smokeToken)))
}

func SetupUIHTML() string {
	return setupUIHTML
}
