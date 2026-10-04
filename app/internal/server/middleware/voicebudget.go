//go:build linux

package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	VoiceBudgetReservationHeader = "X-Edge-Voice-Budget-Reservation"
	VoiceBudgetExpiresAtHeader   = "X-Edge-Voice-Budget-Expires-At"
	VoiceBudgetHMACHeader        = "X-Edge-Voice-Budget-Hmac"
)

type VoiceBudget struct {
	ReservationID string
	ExpiresAt     int64
}

type voiceBudgetCtxKey struct{}

func VoiceBudgetFromContext(ctx context.Context) VoiceBudget {
	budget, _ := ctx.Value(voiceBudgetCtxKey{}).(VoiceBudget)
	return budget
}

func verifiedVoiceBudgetFromRequest(r *http.Request, id Identity, secret string) (VoiceBudget, bool, error) {
	reservation := strings.TrimSpace(r.Header.Get(VoiceBudgetReservationHeader))
	expiry := strings.TrimSpace(r.Header.Get(VoiceBudgetExpiresAtHeader))
	presented := strings.TrimSpace(r.Header.Get(VoiceBudgetHMACHeader))
	if reservation == "" && expiry == "" && presented == "" {
		return VoiceBudget{}, false, nil
	}
	if reservation == "" || len(reservation) > 128 || strings.ContainsAny(reservation, "\r\n") || presented == "" || secret == "" {
		return VoiceBudget{}, true, errors.New("incomplete voice budget")
	}
	if !edgeTimestampFresh(strings.TrimSpace(r.Header.Get("X-Edge-Auth-Ts")), time.Now()) {
		return VoiceBudget{}, true, errors.New("voice budget requires fresh edge authentication")
	}
	expiresAt, err := strconv.ParseInt(expiry, 10, 64)
	now := time.Now().Unix()
	if err != nil || strconv.FormatInt(expiresAt, 10) != expiry || expiresAt <= now || expiresAt > now+14400 {
		return VoiceBudget{}, true, errors.New("invalid voice budget deadline")
	}
	// The caller identity has already passed the base edge HMAC. The
	// budget overlay is independent of registered-agent credentials.
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(strings.Join([]string{"speechkit.voice_budget.v1", id.UserID, id.OrgID, reservation, expiry}, "\n")))
	if !hmacEqual([]byte(presented), []byte(hex.EncodeToString(mac.Sum(nil)))) {
		return VoiceBudget{}, true, errors.New("invalid voice budget signature")
	}
	return VoiceBudget{ReservationID: reservation, ExpiresAt: expiresAt}, true, nil
}
