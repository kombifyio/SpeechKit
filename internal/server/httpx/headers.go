//go:build linux

package httpx

// APIPrefixHeader carries the external API mount prefix after an internal
// route alias rewrites /api/v1/* to the stable /v1/* handler tree.
const APIPrefixHeader = "X-SpeechKit-API-Prefix"
