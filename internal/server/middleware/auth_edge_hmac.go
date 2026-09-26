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

// edgeHMACMaxSkew bounds how far the optional X-Edge-Auth-Ts may deviate from
// server time. It is the replay window: a captured edge-signed header is only
// accepted within this interval once the edge starts sending a timestamp.
const edgeHMACMaxSkew = 5 * time.Minute

func verifyEdgeHMAC(r *http.Request, secret string) (Identity, bool) {
	if secret == "" {
		return Identity{}, false
	}
	presented := strings.TrimSpace(r.Header.Get("X-Edge-Auth-Hmac"))
	userID := strings.TrimSpace(r.Header.Get("X-Edge-User-Id"))
	orgID := strings.TrimSpace(r.Header.Get("X-Edge-Org-Id"))
	plan := strings.TrimSpace(r.Header.Get("X-Edge-Plan"))
	role := strings.TrimSpace(r.Header.Get("X-Edge-Role"))
	ts := strings.TrimSpace(r.Header.Get("X-Edge-Auth-Ts"))
	if presented == "" || userID == "" || orgID == "" {
		return Identity{}, false
	}
	// Signature base: userID + "\n" + orgID + "\n" + plan + "\n" + role,
	// with the timestamp appended as a fifth field ("\n" + ts) when the edge
	// supplies one. Binding the timestamp into the MAC bounds replayability;
	// a request without X-Edge-Auth-Ts uses the legacy unbound base so an
	// older edge signer keeps working (backward compatible). A downgrade —
	// stripping the timestamp from a captured ts-bound request — fails
	// because the presented HMAC was computed over the ts-bound base and
	// will not match the legacy base recomputed here.
	if ts != "" && !edgeTimestampFresh(ts, time.Now()) {
		return Identity{}, false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(userID))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(orgID))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(plan))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(role))
	if ts != "" {
		mac.Write([]byte{'\n'})
		mac.Write([]byte(ts))
	}
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
