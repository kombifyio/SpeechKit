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

//go:embed assets/marks/rosette.png
var rosettePNG []byte

//go:embed assets/marks/k.png
var monogramPNG []byte

const smokeTokenPlaceholder = "__SPEECHKIT_SMOKE_TOKEN__"

// AssistantUIHTML returns the page with the optional demo bearer token
// injected (same contract as the smoke UI on `/`): empty disables
// token-from-page and the UI asks for a bearer token instead.
func AssistantUIHTML(smokeToken string) string {
	return strings.ReplaceAll(assistantHTML, smokeTokenPlaceholder, htmlAttrEscape(smokeToken))
}

// RosettePNG is the standard AI-teal rosette mark asset.
func RosettePNG() []byte { return rosettePNG }

// MonogramPNG is the k monogram mark asset.
func MonogramPNG() []byte { return monogramPNG }

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
