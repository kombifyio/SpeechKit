//go:build linux

package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	adminSessionCookieName = "speechkit_admin_session"
	adminSessionTTL        = 30 * time.Minute
	// adminCSRFCookieName is the JS-readable companion to the admin
	// session cookie. The pair implements double-submit CSRF protection
	// (audit S-13): the SPA reads the cookie value with JS and echoes it
	// into the X-CSRF-Token header on state-changing requests. The
	// browser can read the value because HttpOnly is false, but a
	// cross-site attacker cannot — CORS blocks the read, and SameSite
	// blocks the cookie from being attached to forged cross-origin
	// requests. The header is the proof the request originated from
	// our origin.
	adminCSRFCookieName = "speechkit_admin_csrf"
	// adminCSRFHeaderName is the request header the SPA sends. Picked
	// to match the de-facto convention used by Django, Rails, etc.
	adminCSRFHeaderName = "X-CSRF-Token"
)

// AdminSessionCookieName is exported for server integration tests that verify
// setup/auth transitions without duplicating the cookie contract.
const AdminSessionCookieName = adminSessionCookieName

// AdminCSRFCookieName and AdminCSRFHeaderName are exported so server
// integration tests and the SPA build pipeline can reference the
// double-submit contract without re-declaring it.
const (
	AdminCSRFCookieName = adminCSRFCookieName
	AdminCSRFHeaderName = adminCSRFHeaderName
)

var adminSessionSigningKey = mustRandomBytes(32)

type adminSessionClaims struct {
	User      string `json:"user"`
	ExpiresAt int64  `json:"expires_at"`
	Nonce     string `json:"nonce"`
}

func verifyBasicAdmin(r *http.Request, username, passwordHash string) (Identity, bool) {
	if username == "" || passwordHash == "" {
		return Identity{}, false
	}
	presentedUser, presentedPassword, ok := r.BasicAuth()
	if !ok || presentedUser == "" || presentedPassword == "" {
		return Identity{}, false
	}
	if !hmacEqual([]byte(presentedUser), []byte(username)) {
		return Identity{}, false
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(presentedPassword)) != nil {
		return Identity{}, false
	}
	return Identity{
		UserID: username,
		OrgID:  "default",
		Plan:   "admin",
		Role:   "admin",
		Source: "basic",
	}, true
}

func AdminPasswordMatches(username, passwordHash, presentedUser, presentedPassword string) bool {
	username = strings.TrimSpace(username)
	passwordHash = strings.TrimSpace(passwordHash)
	presentedUser = strings.TrimSpace(presentedUser)
	if username == "" || passwordHash == "" || presentedUser == "" || presentedPassword == "" {
		return false
	}
	return hmacEqual([]byte(presentedUser), []byte(username)) &&
		bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(presentedPassword)) == nil
}

func NewAdminSessionCookie(username, passwordHash string, secure bool, now time.Time) (*http.Cookie, error) {
	username = strings.TrimSpace(username)
	passwordHash = strings.TrimSpace(passwordHash)
	if username == "" || passwordHash == "" {
		return nil, http.ErrNoCookie
	}
	if now.IsZero() {
		now = time.Now()
	}
	expires := now.Add(adminSessionTTL)
	token := signAdminSessionToken(adminSessionClaims{
		User:      username,
		ExpiresAt: expires.Unix(),
		Nonce:     base64.RawURLEncoding.EncodeToString(mustRandomBytes(16)),
	}, passwordHash)
	if token == "" {
		return nil, http.ErrNoCookie
	}
	return &http.Cookie{ // #nosec G124 -- Secure follows the request/proxy scheme so local HTTP admin login keeps working.
		Name:     adminSessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(adminSessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}, nil
}

// NewAdminCSRFCookie returns the JS-readable double-submit CSRF cookie
// that pairs with the admin session cookie minted by
// NewAdminSessionCookie. Callers that issue the session cookie MUST
// also issue this cookie in the same response — otherwise the SPA has
// no token to echo in X-CSRF-Token and EnforceAdminCSRF will refuse
// the next state-changing request from the admin_session identity.
//
// sessionCookieValue is the Value of the session cookie returned by
// NewAdminSessionCookie. The returned CSRF cookie carries
// csrfTokenFor(sessionCookieValue) so the binding HMAC over the
// session signature also covers this cookie.
func NewAdminCSRFCookie(sessionCookieValue string, secure bool, now time.Time) *http.Cookie {
	if strings.TrimSpace(sessionCookieValue) == "" {
		return nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	expires := now.Add(adminSessionTTL)
	return &http.Cookie{ // #nosec G124 -- CSRF double-submit cookie must be JS-readable; SameSite Strict binds it to same-origin requests.
		Name:     adminCSRFCookieName,
		Value:    csrfTokenFor(sessionCookieValue),
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(adminSessionTTL.Seconds()),
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	}
}

func (a authRuntime) handleAdminSessionEndpoint(w http.ResponseWriter, r *http.Request, username, passwordHash string) bool {
	if r == nil || !isAdminSessionPath(r.URL.Path) {
		return false
	}
	switch r.Method {
	case http.MethodGet:
		if id, ok := verifyAdminSession(r, username, passwordHash); ok {
			writeAdminSessionJSON(w, true, id)
			return true
		}
		writeAuthError(w)
		return true
	case http.MethodPost:
		id, ok := verifyBasicAdmin(r, username, passwordHash)
		if !ok {
			writeAuthError(w)
			return true
		}
		a.setAdminSessionCookie(w, r, username, passwordHash)
		writeAdminSessionJSON(w, true, id)
		return true
	case http.MethodDelete:
		a.clearAdminSessionCookie(w, r)
		w.WriteHeader(http.StatusNoContent)
		return true
	case http.MethodOptions:
		w.WriteHeader(http.StatusNoContent)
		return true
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return true
	}
}

func isAdminSessionPath(path string) bool {
	return path == "/v1/admin/session" || path == "/api/v1/admin/session"
}

func verifyAdminSession(r *http.Request, username, passwordHash string) (Identity, bool) {
	if username == "" || passwordHash == "" {
		return Identity{}, false
	}
	cookie, err := r.Cookie(adminSessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return Identity{}, false
	}
	claims, ok := parseAdminSessionToken(cookie.Value, passwordHash)
	if !ok {
		return Identity{}, false
	}
	if time.Now().Unix() > claims.ExpiresAt {
		return Identity{}, false
	}
	if !hmacEqual([]byte(claims.User), []byte(username)) {
		return Identity{}, false
	}
	return Identity{
		UserID: username,
		OrgID:  "default",
		Plan:   "admin",
		Role:   "admin",
		Source: "admin_session",
	}, true
}

func (a authRuntime) setAdminSessionCookie(w http.ResponseWriter, r *http.Request, username, passwordHash string) {
	expires := time.Now().Add(adminSessionTTL)
	secure := a.trustedProxies.RequestIsHTTPS(r)
	token := signAdminSessionToken(adminSessionClaims{
		User:      username,
		ExpiresAt: expires.Unix(),
		Nonce:     base64.RawURLEncoding.EncodeToString(mustRandomBytes(16)),
	}, passwordHash)
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure follows the request/proxy scheme so local HTTP admin login keeps working.
		Name:     adminSessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(adminSessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- CSRF double-submit cookie must be JS-readable; SameSite Strict binds it to same-origin requests.
		Name:  adminCSRFCookieName,
		Value: csrfTokenFor(token),
		Path:  "/",
		// CSRF cookie shares the session lifetime — when the session
		// expires, the cookie disappears with it and a stale token
		// won't validate against a fresh session signature anyway.
		Expires:  expires,
		MaxAge:   int(adminSessionTTL.Seconds()),
		HttpOnly: false, // JS must read this to set the X-CSRF-Token header.
		Secure:   secure,
		// Strict because the cookie is only consumed by our own SPA
		// running on the same origin; tightening from Lax (used by the
		// session cookie for top-level navigation) costs nothing here.
		SameSite: http.SameSiteStrictMode,
	})
}

func (a authRuntime) clearAdminSessionCookie(w http.ResponseWriter, r *http.Request) {
	secure := a.trustedProxies.RequestIsHTTPS(r)
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- deletion cookie mirrors the session cookie attributes for reliable expiry.
		Name:     adminSessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- deletion cookie mirrors the JS-readable CSRF cookie attributes for reliable expiry.
		Name:     adminCSRFCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Unix(0, 0),
		MaxAge:   -1,
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteStrictMode,
	})
}

func writeAdminSessionJSON(w http.ResponseWriter, authenticated bool, id Identity) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"authenticated": authenticated,
		"identity":      id,
		"expires_in":    int(adminSessionTTL.Seconds()),
	})
}

func signAdminSessionToken(claims adminSessionClaims, passwordHash string) string {
	payload, err := json.Marshal(claims)
	if err != nil {
		return ""
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, adminSessionSigningSecret(passwordHash))
	mac.Write([]byte(encodedPayload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encodedPayload + "." + signature
}

func parseAdminSessionToken(token, passwordHash string) (adminSessionClaims, bool) {
	payload, signature, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || payload == "" || signature == "" {
		return adminSessionClaims{}, false
	}
	mac := hmac.New(sha256.New, adminSessionSigningSecret(passwordHash))
	mac.Write([]byte(payload))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmacEqual([]byte(signature), []byte(want)) {
		return adminSessionClaims{}, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return adminSessionClaims{}, false
	}
	var claims adminSessionClaims
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return adminSessionClaims{}, false
	}
	return claims, true
}

func adminSessionSigningSecret(passwordHash string) []byte {
	mac := hmac.New(sha256.New, adminSessionSigningKey)
	mac.Write([]byte(strings.TrimSpace(passwordHash)))
	return mac.Sum(nil)
}

func mustRandomBytes(size int) []byte {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		panic("speechkit admin session entropy unavailable: " + err.Error())
	}
	return buf
}
