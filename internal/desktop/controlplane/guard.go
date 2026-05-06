package controlplane

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const (
	// MaxBodySize caps mutating local control-plane requests. Control-plane
	// POST/PATCH bodies are small forms; 1 MB is intentionally generous.
	MaxBodySize = 1 << 20

	TokenCookieName = "speechkit_control_plane"
	TokenHeaderName = "X-SpeechKit-Control-Token"
)

// Guard rejects cross-site and disallowed-origin mutating requests. It is the
// primary CSRF defence for the local desktop control plane.
func Guard(next http.Handler, sessionToken string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sessionToken != "" {
			SetTokenBootstrap(w, sessionToken)
		}

		if !IsMutatingMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, MaxBodySize)

		if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "cross-site") {
			http.Error(w, "cross-site requests are not allowed", http.StatusForbidden)
			return
		}

		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" && !IsAllowedOrigin(origin) {
			http.Error(w, "origin is not allowed", http.StatusForbidden)
			return
		}

		if sessionToken != "" && !HasValidTokenHeader(r, sessionToken) {
			http.Error(w, "control-plane session token is invalid", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func NewToken() string {
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		panic(fmt.Sprintf("generate control-plane token: %v", err))
	}
	return hex.EncodeToString(token[:])
}

func SetTokenBootstrap(w http.ResponseWriter, token string) {
	w.Header().Set(TokenHeaderName, token)
	http.SetCookie(w, &http.Cookie{
		Name:     TokenCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

func HasValidTokenHeader(r *http.Request, expected string) bool {
	presented := strings.TrimSpace(r.Header.Get(TokenHeaderName))
	expected = strings.TrimSpace(expected)
	if presented == "" || expected == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) == 1
}

func IsMutatingMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func IsAllowedOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	if host == "wails.localhost" || strings.HasSuffix(host, ".wails.localhost") {
		return true
	}
	return false
}
