// Package assistantui embeds the built /assistant web page — the server-side
// surface of the standard Voice Assistant UI module. The HTML is generated
// from clients/typescript/apps/assistant-web (kit element + voiceagent
// browser client inlined into one file); regenerate with
// `pnpm --filter @kombifyio/speechkit-assistant-web build`.
package assistantui

import (
	_ "embed"
	"strings"
)

//go:embed assets/assistant.html
var assistantHTML string

// MarksOrigin is the Cloudflare R2 public origin that serves the two mark
// images. The page references them there; this server never serves image or
// other file bytes (D-2026-10-25-03).
const MarksOrigin = "https://assets.kombify.io"

// markPaths maps the page's relative mark paths to their R2 object paths.
var markPaths = strings.NewReplacer(
	"/assistant/marks/rosette.png", MarksOrigin+"/speechkit/assistant/marks/rosette.png",
	"/assistant/marks/k.png", MarksOrigin+"/speechkit/assistant/marks/k.png",
)

const smokeTokenPlaceholder = "__SPEECHKIT_SMOKE_TOKEN__"

// AssistantUIHTML returns the page with the optional demo bearer token
// injected (same contract as the smoke UI on `/`): empty disables
// token-from-page and the UI asks for a bearer token instead.
func AssistantUIHTML(smokeToken string) string {
	page := strings.ReplaceAll(assistantHTML, smokeTokenPlaceholder, htmlAttrEscape(smokeToken))
	return markPaths.Replace(page)
}

func htmlAttrEscape(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		`"`, "&quot;",
		"'", "&#39;",
		"<", "&lt;",
		">", "&gt;",
	)
	return replacer.Replace(value)
}
