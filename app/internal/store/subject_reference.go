package store

import (
	"crypto/sha256"
	"encoding/base64"
)

// SubjectReference derives the versioned opaque account reference used by rights participants.
func SubjectReference(subject string) string {
	sum := sha256.Sum256([]byte("kombify:subject:v1:" + subject))
	return "sr1_" + base64.RawURLEncoding.EncodeToString(sum[:])
}
