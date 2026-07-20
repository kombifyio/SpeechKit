package wssession

import (
	"net/url"
	"strings"
)

// WebSocketURL builds the externally visible ws(s):// URL for a session
// endpoint mounted under the versioned API base. relPath is the path below
// that base, e.g. "/voiceagent/sessions/<id>/ws" or
// "/dictation/stream/sessions/<id>/ws".
//
// The configured publicURL (when parseable) wins over request-derived
// scheme/host so Docker/proxy deployments return a browser-reachable URL
// rather than a container hostname. requestIsHTTPS is the caller's
// trusted-proxy-aware TLS determination for the incoming request;
// apiPrefixHeader is the raw value of the httpx.APIPrefixHeader request
// header (passed in, not read here, so this package stays free of the
// linux-only httpx import and natively testable).
func WebSocketURL(requestHost, apiPrefixHeader, publicURL string, requestIsHTTPS bool, relPath string) string {
	scheme := "ws"
	if requestIsHTTPS {
		scheme = "wss"
	}
	host := sanitizeRequestHost(requestHost)
	if host == "" {
		host = "localhost"
	}
	prefix := cleanPublicAPIPrefix(apiPrefixHeader)
	if strings.TrimSpace(publicURL) != "" {
		if parsed, ok := parsePublicURL(publicURL); ok {
			scheme = parsed.scheme
			host = parsed.host
			prefix = parsed.prefix
		}
	}
	if !strings.HasPrefix(relPath, "/") {
		relPath = "/" + relPath
	}
	return scheme + "://" + host + publicAPIBase(prefix) + relPath
}

type publicURLParts struct {
	scheme string
	host   string
	prefix string
}

func parsePublicURL(raw string) (publicURLParts, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return publicURLParts{}, false
	}
	var scheme string
	switch strings.ToLower(u.Scheme) {
	case "https", "wss":
		scheme = "wss"
	case "http", "ws":
		scheme = "ws"
	default:
		return publicURLParts{}, false
	}
	return publicURLParts{
		scheme: scheme,
		host:   u.Host,
		prefix: cleanPublicAPIPrefix(u.EscapedPath()),
	}, true
}

func sanitizeRequestHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, "/\\@ \t\r\n") {
		return ""
	}
	return host
}

func cleanPublicAPIPrefix(prefix string) string {
	prefix = strings.TrimRight(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		return ""
	}
	if prefix == "/api" || prefix == "/api/v1" || prefix == "/v1" || strings.HasPrefix(prefix, "/v1/") {
		return prefix
	}
	return ""
}

func publicAPIBase(prefix string) string {
	switch prefix {
	case "":
		return "/v1"
	case "/api":
		return "/api/v1"
	default:
		return prefix
	}
}
