package main

import (
	"strings"
)

// Environment/config parsing for the MCP command only.
// No server startup, protocol handling, or embedded spec fallback belongs here.
type serverOptions struct {
	modes     map[string]bool
	serverURL string
	token     string
	transport string
	mcpToken  string
}

func parseModes(raw string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			out[part] = true
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
