package claimstore

import (
	"crypto/subtle"
	"strings"
	"time"
)

func (l *Ledger) validateKey(key Key, now time.Time) (Key, error) {
	key.PairedDeviceID = strings.TrimSpace(key.PairedDeviceID)
	key.RequestID = strings.ToLower(strings.TrimSpace(key.RequestID))
	if err := validatePairedDeviceID(key.PairedDeviceID); err != nil {
		return Key{}, err
	}
	if _, err := ValidateRequestID(key.RequestID, now, l.options.MaxRequestAge, l.options.FutureSkew); err != nil {
		return Key{}, err
	}
	return key, nil
}

func validatePairedDeviceID(id string) error {
	if id == "" || len(id) > maxPairedDeviceIDBytes {
		return ErrInvalidKey
	}
	for _, character := range id {
		switch {
		case character >= 'a' && character <= 'z':
		case character >= 'A' && character <= 'Z':
		case character >= '0' && character <= '9':
		case character == '.', character == '_', character == '-', character == ':':
		default:
			return ErrInvalidKey
		}
	}
	return nil
}

func validateHandle(handle Handle) error {
	if err := validatePairedDeviceID(handle.key.PairedDeviceID); err != nil || handle.key.RequestID == "" {
		return ErrInvalidKey
	}
	if isZeroDigest(handle.digest) {
		return ErrInvalidDigest
	}
	return nil
}

func isZeroDigest(digest Digest) bool {
	var zero Digest
	return subtle.ConstantTimeCompare(digest[:], zero[:]) == 1
}

func sameDigest(stored []byte, candidate Digest) bool {
	return len(stored) == len(candidate) && subtle.ConstantTimeCompare(stored, candidate[:]) == 1
}
