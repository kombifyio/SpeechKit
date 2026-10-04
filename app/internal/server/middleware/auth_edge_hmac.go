//go:build linux

package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// edgeHMACMaxSkew bounds how far an edge signing timestamp may deviate from
// server time. It is the freshness window of the signed edge envelope and of
// the legacy timestamped HMAC.
const edgeHMACMaxSkew = 5 * time.Minute

// verifyEdgeHMAC accepts the legacy X-Edge-Auth-Hmac identity signature during
// the migration to the signed edge envelope (auth_edge_envelope.go). It binds
// no key id, method, path or nonce, so a captured value stays replayable
// inside the window; it is accepted only while the legacy flag is on and only
// with a fresh X-Edge-Auth-Ts bound as the fifth field.
//
// Signature base: userID LF orgID LF plan LF role LF ts, hex HMAC-SHA256.
func verifyEdgeHMAC(r *http.Request, secret string) (Identity, bool) {
	if secret == "" {
		return Identity{}, false
	}
	presented := strings.TrimSpace(r.Header.Get("X-Edge-Auth-Hmac"))
	userID := strings.TrimSpace(r.Header.Get("X-Edge-User-Id"))
	orgID := strings.TrimSpace(r.Header.Get("X-Edge-Org-Id"))
	plan := strings.TrimSpace(r.Header.Get("X-Edge-Plan"))
	role := strings.TrimSpace(r.Header.Get("X-Edge-Role"))
	ts := strings.TrimSpace(r.Header.Get(EdgeAuthTimestampHeader))
	if presented == "" || userID == "" || orgID == "" || !edgeTimestampFresh(ts, time.Now()) {
		return Identity{}, false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.Join([]string{userID, orgID, plan, role, ts}, "\n")))
	want := hex.EncodeToString(mac.Sum(nil))
	if !hmacEqual([]byte(presented), []byte(want)) {
		return Identity{}, false
	}
	return Identity{
		UserID: userID,
		OrgID:  orgID,
		Plan:   plan,
		Role:   role,
		Source: "edge_hmac",
	}, true
}

// edgeTimestampFresh reports whether ts (Unix seconds as a decimal string) is
// within edgeHMACMaxSkew of now. A malformed timestamp is rejected so a
// garbage value cannot bypass the freshness check.
func edgeTimestampFresh(ts string, now time.Time) bool {
	secs, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	delta := now.Sub(time.Unix(secs, 0))
	if delta < 0 {
		delta = -delta
	}
	return delta <= edgeHMACMaxSkew
}
