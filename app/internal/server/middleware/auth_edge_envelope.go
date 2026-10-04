//go:build linux

package middleware

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Signed edge envelope. A trusted edge (reverse proxy or edge worker) that has
// already authenticated the caller forwards the identity in X-Edge-User-Id,
// X-Edge-Org-Id, X-Edge-Plan and X-Edge-Role and signs it together with the
// request it belongs to:
//
//	X-Edge-Auth-Signature:   v2=<base64url(HMAC-SHA256(secret, payload))>
//	X-Edge-Auth-Key-Id:      id of the signing key (rotation slot)
//	X-Edge-Auth-Ts:          signing time, Unix seconds
//	X-Edge-Auth-Nonce:       unique value per request
//	X-Edge-Auth-Signed-Path: request path and query as the server receives it
//
// payload = "speechkit.edge_auth.v2" LF key id LF METHOD LF signed path LF
// user LF org LF plan LF role LF ts LF nonce.
//
// The server accepts an envelope only for a known key id, within
// edgeHMACMaxSkew of its own clock, for the method and path it was signed for,
// and only once: a nonce seen inside the window is rejected. The request body
// is not bound; the path, method, identity and freshness are.
const (
	EdgeAuthSignatureHeader  = "X-Edge-Auth-Signature"
	EdgeAuthKeyIDHeader      = "X-Edge-Auth-Key-Id"
	EdgeAuthTimestampHeader  = "X-Edge-Auth-Ts"
	EdgeAuthNonceHeader      = "X-Edge-Auth-Nonce"
	EdgeAuthSignedPathHeader = "X-Edge-Auth-Signed-Path"

	edgeEnvelopeVersion = "v2"
	edgeEnvelopeDomain  = "speechkit.edge_auth.v2"
	edgeNonceMaxLen     = 128

	// EdgeAuthKeyIDEnv names the key id of the primary edge secret;
	// EdgeAuthNextKeyIDEnv the id of the rotation secret held in
	// "<edge secret env>_NEXT".
	EdgeAuthKeyIDEnv     = "EDGE_AUTH_KEY_ID"
	EdgeAuthNextKeyIDEnv = "EDGE_AUTH_KEY_ID_NEXT"

	defaultEdgeKeyID     = "primary"
	defaultEdgeNextKeyID = "next"
)

// EdgeKey is one accepted edge signing key.
type EdgeKey struct {
	ID     string
	Secret string
}

// edgeKeysFromEnv returns the primary key and, when configured, the rotation
// key. Key ids default to "primary" and "next".
func edgeKeysFromEnv(primarySecret, secretEnv string) []EdgeKey {
	keys := make([]EdgeKey, 0, 2)
	if secret := strings.TrimSpace(primarySecret); secret != "" {
		keys = append(keys, EdgeKey{ID: envOr(EdgeAuthKeyIDEnv, defaultEdgeKeyID), Secret: secret})
	}
	if secretEnv = strings.TrimSpace(secretEnv); secretEnv != "" {
		if next := strings.TrimSpace(os.Getenv(secretEnv + "_NEXT")); next != "" {
			keys = append(keys, EdgeKey{ID: envOr(EdgeAuthNextKeyIDEnv, defaultEdgeNextKeyID), Secret: next})
		}
	}
	return keys
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func hasEdgeEnvelope(r *http.Request) bool {
	return strings.TrimSpace(r.Header.Get(EdgeAuthSignatureHeader)) != ""
}

func verifyEdgeEnvelope(r *http.Request, keys []EdgeKey, replay *edgeReplayGuard, now time.Time) (Identity, bool) {
	signature := strings.TrimSpace(r.Header.Get(EdgeAuthSignatureHeader))
	keyID := strings.TrimSpace(r.Header.Get(EdgeAuthKeyIDHeader))
	ts := strings.TrimSpace(r.Header.Get(EdgeAuthTimestampHeader))
	nonce := strings.TrimSpace(r.Header.Get(EdgeAuthNonceHeader))
	signedPath := strings.TrimSpace(r.Header.Get(EdgeAuthSignedPathHeader))
	userID := strings.TrimSpace(r.Header.Get("X-Edge-User-Id"))
	orgID := strings.TrimSpace(r.Header.Get("X-Edge-Org-Id"))
	plan := strings.TrimSpace(r.Header.Get("X-Edge-Plan"))
	role := strings.TrimSpace(r.Header.Get("X-Edge-Role"))
	if signature == "" || keyID == "" || ts == "" || nonce == "" || len(nonce) > edgeNonceMaxLen || signedPath == "" || userID == "" || orgID == "" || replay == nil {
		return Identity{}, false
	}
	presented, ok := strings.CutPrefix(signature, edgeEnvelopeVersion+"=")
	if !ok || presented == "" {
		return Identity{}, false
	}
	secret := ""
	for _, key := range keys {
		if key.ID == keyID && key.Secret != "" {
			secret = key.Secret
			break
		}
	}
	if secret == "" {
		return Identity{}, false
	}
	if signedPath != requestSignedPath(r) {
		return Identity{}, false
	}
	secs, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || !edgeTimestampFresh(ts, now) {
		return Identity{}, false
	}
	payload := edgeEnvelopePayload(keyID, r.Method, signedPath, userID, orgID, plan, role, ts, nonce)
	if !hmacEqual([]byte(presented), []byte(signEdgePayload(secret, payload))) {
		return Identity{}, false
	}
	// Claim the nonce only after the signature verified, so unauthenticated
	// traffic cannot fill the guard.
	if !replay.claim(keyID+"\x00"+nonce, time.Unix(secs, 0).Add(edgeHMACMaxSkew), now) {
		return Identity{}, false
	}
	return Identity{UserID: userID, OrgID: orgID, Plan: plan, Role: role, Source: "edge_hmac"}, true
}

func edgeEnvelopePayload(keyID, method, signedPath, userID, orgID, plan, role, ts, nonce string) string {
	return strings.Join([]string{edgeEnvelopeDomain, keyID, strings.ToUpper(method), signedPath, userID, orgID, plan, role, ts, nonce}, "\n")
}

func signEdgePayload(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// requestSignedPath is the escaped path plus query the server received, the
// value an edge signs as X-Edge-Auth-Signed-Path.
func requestSignedPath(r *http.Request) string {
	path := r.URL.EscapedPath()
	if path == "" {
		path = "/"
	}
	if r.URL.RawQuery != "" {
		path += "?" + r.URL.RawQuery
	}
	return path
}

// SignEdgeEnvelope sets a signed edge envelope for id on r, bound to r's
// method and path. It is the reference signer for edge implementations and
// tests; production edges sign the same payload in their own runtime.
func SignEdgeEnvelope(r *http.Request, key EdgeKey, id Identity, signedAt time.Time) error {
	if r == nil || strings.TrimSpace(key.ID) == "" || strings.TrimSpace(key.Secret) == "" || strings.TrimSpace(id.UserID) == "" || strings.TrimSpace(id.OrgID) == "" {
		return errors.New("edge envelope: request, key and identity are required")
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return err
	}
	nonce := hex.EncodeToString(nonceBytes)
	ts := strconv.FormatInt(signedAt.Unix(), 10)
	signedPath := requestSignedPath(r)
	r.Header.Set("X-Edge-User-Id", id.UserID)
	r.Header.Set("X-Edge-Org-Id", id.OrgID)
	r.Header.Set("X-Edge-Plan", id.Plan)
	if id.Role != "" {
		r.Header.Set("X-Edge-Role", id.Role)
	} else {
		r.Header.Del("X-Edge-Role")
	}
	r.Header.Set(EdgeAuthKeyIDHeader, key.ID)
	r.Header.Set(EdgeAuthTimestampHeader, ts)
	r.Header.Set(EdgeAuthNonceHeader, nonce)
	r.Header.Set(EdgeAuthSignedPathHeader, signedPath)
	payload := edgeEnvelopePayload(key.ID, r.Method, signedPath, id.UserID, id.OrgID, id.Plan, id.Role, ts, nonce)
	r.Header.Set(EdgeAuthSignatureHeader, edgeEnvelopeVersion+"="+signEdgePayload(key.Secret, payload))
	return nil
}
