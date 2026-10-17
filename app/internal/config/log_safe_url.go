package config

import (
	"net/url"
	"strings"
)

// LogSafeURL returns a non-secret hint of an operator-supplied endpoint URL
// for startup logs: scheme, host and path only. Userinfo (user:password@),
// the query string (?api_key=...) and the fragment are dropped, because an
// endpoint configured through env or config can carry credentials there. A
// value that does not parse as an absolute URL is replaced by a placeholder
// rather than echoed.
func LogSafeURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "<unparseable url>"
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: parsed.Path}).String()
}
