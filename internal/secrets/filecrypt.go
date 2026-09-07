package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// The envelope written to disk is
//
//	version(1) || nonce(12) || AES-256-GCM ciphertext+tag
//
// The version byte is authenticated as additional data, so a blob resealed
// under a future format cannot be replayed as this one. It exists so a later
// master-key rotation can add version 2 without guessing at the layout of the
// files already on disk.
const (
	secretEnvelopeVersion = 1
	secretMasterKeyLen    = 32
)

var (
	// errSecretKeySize means the caller handed the envelope helpers something
	// other than a 32-byte AES-256 key. The key itself is never reported.
	errSecretKeySize = errors.New("secrets: master key must be 32 bytes")
	// errSecretEnvelopeVersion means the blob on disk was written by a format
	// this build does not know how to open.
	errSecretEnvelopeVersion = errors.New("secrets: unsupported secret envelope version")
	// errSecretEnvelopeTruncated means the blob is too short to hold a nonce
	// and an authentication tag.
	errSecretEnvelopeTruncated = errors.New("secrets: secret envelope is truncated")
	// errSecretEnvelopeCorrupt means authentication failed: the wrong master
	// key, a tampered file, or a blob from another machine.
	errSecretEnvelopeCorrupt = errors.New("secrets: secret envelope failed authentication")
)

// sealSecret encrypts plaintext under a 32-byte master key. An empty payload
// seals to an empty blob, matching the DPAPI backend so both platforms treat a
// zero-length secret file the same way.
func sealSecret(key, plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, nil
	}
	aead, err := newSecretAEAD(key)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secrets: generate secret nonce: %w", err)
	}

	header := []byte{secretEnvelopeVersion}
	envelope := make([]byte, 0, len(header)+len(nonce)+len(plaintext)+aead.Overhead())
	envelope = append(envelope, header...)
	envelope = append(envelope, nonce...)
	return aead.Seal(envelope, nonce, plaintext, header), nil
}

// openSecret decrypts an envelope produced by sealSecret. Errors are typed and
// never carry plaintext, ciphertext or key bytes.
func openSecret(key, envelope []byte) ([]byte, error) {
	if len(envelope) == 0 {
		return nil, nil
	}
	aead, err := newSecretAEAD(key)
	if err != nil {
		return nil, err
	}

	header := envelope[:1]
	if header[0] != secretEnvelopeVersion {
		return nil, fmt.Errorf("%w: %d", errSecretEnvelopeVersion, header[0])
	}
	if len(envelope) < 1+aead.NonceSize()+aead.Overhead() {
		return nil, errSecretEnvelopeTruncated
	}

	nonce := envelope[1 : 1+aead.NonceSize()]
	plain, err := aead.Open(nil, nonce, envelope[1+aead.NonceSize():], header)
	if err != nil {
		// cipher's message is deliberately dropped: it says nothing useful and
		// keeping it out avoids ever printing envelope bytes.
		return nil, errSecretEnvelopeCorrupt
	}
	return plain, nil
}

func newSecretAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != secretMasterKeyLen {
		return nil, errSecretKeySize
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: aes cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: aes-gcm: %w", err)
	}
	return aead, nil
}
