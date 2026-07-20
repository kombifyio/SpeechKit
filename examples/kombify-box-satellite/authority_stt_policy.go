package main

import "strings"

// localAuthoritySTTProvider is deliberately closed. Realtime Home Assistant
// authority must be independent from the remote conversation provider and
// must not acquire a cloud or gateway fallback.
func localAuthoritySTTProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "local", "whispercpp", "whisper.cpp", "stt.local.whispercpp":
		return true
	default:
		return false
	}
}
