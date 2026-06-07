//go:build linux

package voiceagent

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	defaultLiveKitTokenTTL    = 10 * time.Minute
	defaultLiveKitRoomPrefix  = "speechkit-va"
	liveKitParticipantPrefix  = "speechkit"
	liveKitClockSkewAllowance = 5 * time.Second
)

// LiveKitTokenIssuer mints short-lived LiveKit join tokens for authenticated
// SpeechKit voice-agent sessions. It deliberately does not create rooms via
// the LiveKit admin API: a scoped join token is enough for LiveKit to lazily
// create self-hosted rooms, while keeping this path independent of Twirp.
type LiveKitTokenIssuer struct {
	URL        string
	APIKey     string
	APISecret  string
	TokenTTL   time.Duration
	RoomPrefix string
	Clock      func() time.Time
}

type LiveKitJoinInfo struct {
	URL                 string `json:"url"`
	Room                string `json:"room"`
	Token               string `json:"token"`
	TokenExpiresAt      string `json:"token_expires_at"`
	ParticipantIdentity string `json:"participant_identity"`
}

type liveKitClaims struct {
	Issuer    string            `json:"iss"`
	Subject   string            `json:"sub"`
	NotBefore int64             `json:"nbf"`
	ExpiresAt int64             `json:"exp"`
	JWTID     string            `json:"jti"`
	Name      string            `json:"name,omitempty"`
	Metadata  string            `json:"metadata,omitempty"`
	Video     liveKitVideoGrant `json:"video"`
}

type liveKitVideoGrant struct {
	Room                 string   `json:"room"`
	RoomJoin             bool     `json:"roomJoin"`
	CanPublish           bool     `json:"canPublish"`
	CanPublishData       bool     `json:"canPublishData"`
	CanSubscribe         bool     `json:"canSubscribe"`
	CanUpdateOwnMetadata bool     `json:"canUpdateOwnMetadata"`
	CanPublishSources    []string `json:"canPublishSources,omitempty"`
}

func (i *LiveKitTokenIssuer) Enabled() bool {
	return i != nil && strings.TrimSpace(i.URL) != ""
}

func (i *LiveKitTokenIssuer) IssueJoinToken(_ context.Context, sessionID string, owner Identity) (LiveKitJoinInfo, error) {
	if i == nil || !i.Enabled() {
		return LiveKitJoinInfo{}, errors.New("voiceagent: LiveKit token issuer is disabled")
	}
	apiKey := strings.TrimSpace(i.APIKey)
	apiSecret := strings.TrimSpace(i.APISecret)
	if apiKey == "" || apiSecret == "" {
		return LiveKitJoinInfo{}, errors.New("voiceagent: LiveKit API key or secret is not configured")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return LiveKitJoinInfo{}, errors.New("voiceagent: LiveKit token requires a session id")
	}
	if strings.TrimSpace(owner.UserID) == "" {
		return LiveKitJoinInfo{}, errors.New("voiceagent: LiveKit token requires an owner user id")
	}

	now := i.now().UTC()
	ttl := i.TokenTTL
	if ttl <= 0 {
		ttl = defaultLiveKitTokenTTL
	}
	expires := now.Add(ttl)
	room := i.roomName(sessionID)
	identity := liveKitParticipantIdentity(sessionID, owner.UserID)
	metadata, err := json.Marshal(map[string]string{
		"session_id": sessionID,
		"user_id":    owner.UserID,
		"org_id":     owner.OrgID,
		"plan":       owner.Plan,
		"role":       owner.Role,
	})
	if err != nil {
		return LiveKitJoinInfo{}, err
	}
	claims := liveKitClaims{
		Issuer:    apiKey,
		Subject:   identity,
		NotBefore: now.Add(-liveKitClockSkewAllowance).Unix(),
		ExpiresAt: expires.Unix(),
		JWTID:     randomHex(16),
		Name:      owner.UserID,
		Metadata:  string(metadata),
		Video: liveKitVideoGrant{
			Room:                 room,
			RoomJoin:             true,
			CanPublish:           true,
			CanPublishData:       true,
			CanSubscribe:         true,
			CanUpdateOwnMetadata: false,
			CanPublishSources:    []string{"microphone"},
		},
	}
	token, err := signLiveKitJWT(apiSecret, claims)
	if err != nil {
		return LiveKitJoinInfo{}, err
	}
	return LiveKitJoinInfo{
		URL:                 strings.TrimRight(strings.TrimSpace(i.URL), "/"),
		Room:                room,
		Token:               token,
		TokenExpiresAt:      expires.Format(time.RFC3339),
		ParticipantIdentity: identity,
	}, nil
}

func (i *LiveKitTokenIssuer) now() time.Time {
	if i.Clock != nil {
		return i.Clock()
	}
	return time.Now()
}

func (i *LiveKitTokenIssuer) roomName(sessionID string) string {
	prefix := strings.Trim(strings.TrimSpace(i.RoomPrefix), "-_ ")
	if prefix == "" {
		prefix = defaultLiveKitRoomPrefix
	}
	return prefix + "-" + compactTokenPart(sessionID, 48)
}

func liveKitParticipantIdentity(sessionID, userID string) string {
	userHash := sha256.Sum256([]byte(strings.TrimSpace(userID)))
	session := compactTokenPart(sessionID, 18)
	return fmt.Sprintf("%s-%s-%s", liveKitParticipantPrefix, hex.EncodeToString(userHash[:6]), session)
}

func signLiveKitJWT(secret string, claims liveKitClaims) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "HS256", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(unsigned))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return unsigned + "." + sig, nil
}

func randomHex(n int) string {
	if n <= 0 {
		n = 16
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func compactTokenPart(value string, maxLen int) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = randomHex(8)
	}
	if maxLen > 0 && len(out) > maxLen {
		out = out[:maxLen]
	}
	return strings.Trim(out, "-")
}
