package claimstore

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"strings"
)

const (
	requestDigestDomain = "speechkit.ha_assist.claim.v1"
	minimumDigestKeyLen = 32
	maxCommandTextBytes = 16 * 1024
)

var (
	// ErrDigestKeyTooShort prevents low-entropy pairing material from being
	// used directly as the HMAC key.
	ErrDigestKeyTooShort = errors.New("claimstore: digest key must contain at least 32 bytes")
	// ErrInvalidCanonicalRequest reports a missing or over-sized field in the
	// exact request representation that will be sent to Home Assistant.
	ErrInvalidCanonicalRequest = errors.New("claimstore: invalid canonical request")
)

// CanonicalRequest contains every client-controlled value that can affect the
// Home Assistant command. Callers must dispatch these normalized values, not
// the pre-normalization HTTP payload, so fingerprinting and execution cannot
// diverge.
type CanonicalRequest struct {
	PairedDeviceID string
	RequestID      string
	RuleID         string
	Locale         string
	Text           string
	EntityID       string
	ExpectedState  string
}

// NormalizeCanonicalRequest trims the bounded textual fields used by both the
// request digest and the outbound Home Assistant request.
func NormalizeCanonicalRequest(req CanonicalRequest) (CanonicalRequest, error) {
	req.PairedDeviceID = strings.TrimSpace(req.PairedDeviceID)
	req.RequestID = strings.ToLower(strings.TrimSpace(req.RequestID))
	req.RuleID = strings.TrimSpace(req.RuleID)
	req.Locale = strings.TrimSpace(req.Locale)
	req.Text = strings.TrimSpace(req.Text)
	req.EntityID = strings.TrimSpace(req.EntityID)
	req.ExpectedState = strings.TrimSpace(req.ExpectedState)

	if err := validatePairedDeviceID(req.PairedDeviceID); err != nil {
		return CanonicalRequest{}, fmt.Errorf("%w: %w", ErrInvalidCanonicalRequest, err)
	}
	if req.RequestID == "" || len(req.RequestID) > 128 {
		return CanonicalRequest{}, fmt.Errorf("%w: request id is required and must be bounded", ErrInvalidCanonicalRequest)
	}
	if err := validatePairedDeviceID(req.RuleID); err != nil {
		return CanonicalRequest{}, fmt.Errorf("%w: rule id is required and must be bounded", ErrInvalidCanonicalRequest)
	}
	if req.Locale == "" || len(req.Locale) > maxLanguageBytes || !validLanguage(req.Locale) {
		return CanonicalRequest{}, fmt.Errorf("%w: locale is required and must be bounded", ErrInvalidCanonicalRequest)
	}
	if req.Text == "" || len(req.Text) > maxCommandTextBytes {
		return CanonicalRequest{}, fmt.Errorf("%w: text is required and must be at most %d bytes", ErrInvalidCanonicalRequest, maxCommandTextBytes)
	}
	if !validCanonicalLightEntityID(req.EntityID) {
		return CanonicalRequest{}, fmt.Errorf("%w: entity id must name one explicit light", ErrInvalidCanonicalRequest)
	}
	if req.ExpectedState != "on" && req.ExpectedState != "off" {
		return CanonicalRequest{}, fmt.Errorf("%w: expected state must be on or off", ErrInvalidCanonicalRequest)
	}
	return req, nil
}

func validCanonicalLightEntityID(value string) bool {
	const prefix = "light."
	if !strings.HasPrefix(value, prefix) || len(value) <= len(prefix) || len(value) > 128 {
		return false
	}
	for index := len(prefix); index < len(value); index++ {
		character := value[index]
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

// HMACDigest computes the persisted request fingerprint. Length-prefixing
// avoids delimiter ambiguity, while the domain prefix prevents cross-protocol
// reuse. The HMAC key is not retained by the store.
func HMACDigest(key []byte, req CanonicalRequest) (Digest, error) {
	if len(key) < minimumDigestKeyLen {
		return Digest{}, ErrDigestKeyTooShort
	}
	normalized, err := NormalizeCanonicalRequest(req)
	if err != nil {
		return Digest{}, err
	}

	mac := hmac.New(sha256.New, key)
	writeDigestField(mac, requestDigestDomain)
	writeDigestField(mac, normalized.PairedDeviceID)
	writeDigestField(mac, normalized.RequestID)
	writeDigestField(mac, normalized.RuleID)
	writeDigestField(mac, normalized.Locale)
	writeDigestField(mac, normalized.Text)
	writeDigestField(mac, normalized.EntityID)
	writeDigestField(mac, normalized.ExpectedState)

	var out Digest
	copy(out[:], mac.Sum(nil))
	return out, nil
}

func writeDigestField(dst hash.Hash, value string) {
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], uint32(len(value)))
	_, _ = dst.Write(size[:])
	_, _ = dst.Write([]byte(value))
}
