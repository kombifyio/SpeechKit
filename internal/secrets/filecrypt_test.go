package secrets

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func testMasterKey(fill byte) []byte {
	key := make([]byte, secretMasterKeyLen)
	for i := range key {
		key[i] = fill + byte(i)
	}
	return key
}

func TestSealSecretRoundTrips(t *testing.T) {
	key := testMasterKey(0x11)

	for name, plaintext := range map[string]string{
		"short":            "hf_token",
		"whitespace":       "  padded token  ",
		"unicode":          "clé-secrète-🔐",
		"aes block length": strings.Repeat("a", 16),
		"long":             strings.Repeat("token-", 512),
	} {
		t.Run(name, func(t *testing.T) {
			envelope, err := sealSecret(key, []byte(plaintext))
			if err != nil {
				t.Fatalf("sealSecret: %v", err)
			}
			if bytes.Contains(envelope, []byte(plaintext)) {
				t.Fatal("envelope leaked the plaintext")
			}
			plain, err := openSecret(key, envelope)
			if err != nil {
				t.Fatalf("openSecret: %v", err)
			}
			assertTestSecret(t, "decrypted secret", string(plain), plaintext)
		})
	}
}

func TestSealSecretUsesAFreshNoncePerCall(t *testing.T) {
	key := testMasterKey(0x22)

	first, err := sealSecret(key, []byte("same-secret"))
	if err != nil {
		t.Fatalf("sealSecret: %v", err)
	}
	second, err := sealSecret(key, []byte("same-secret"))
	if err != nil {
		t.Fatalf("sealSecret: %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("two seals of the same secret produced the same envelope")
	}
}

func TestSealSecretTreatsEmptyPayloadLikeDPAPI(t *testing.T) {
	key := testMasterKey(0x33)

	envelope, err := sealSecret(key, nil)
	if err != nil || envelope != nil {
		t.Fatalf("sealSecret(empty) = (%v, %v), want nil nil", envelope, err)
	}
	plain, err := openSecret(key, nil)
	if err != nil || plain != nil {
		t.Fatalf("openSecret(empty) = (%v, %v), want nil nil", plain, err)
	}
}

func TestOpenSecretRejectsUnusableEnvelopes(t *testing.T) {
	key := testMasterKey(0x44)
	envelope, err := sealSecret(key, []byte("secret-value"))
	if err != nil {
		t.Fatalf("sealSecret: %v", err)
	}

	wrongVersion := append([]byte(nil), envelope...)
	wrongVersion[0] = secretEnvelopeVersion + 1

	flippedTag := append([]byte(nil), envelope...)
	flippedTag[len(flippedTag)-1] ^= 0xFF

	garbage := []byte{secretEnvelopeVersion, 0x00, 0x01, 0x02, 0x03}

	for name, tc := range map[string]struct {
		key      []byte
		envelope []byte
		want     error
	}{
		"wrong key":         {key: testMasterKey(0x55), envelope: envelope, want: errSecretEnvelopeCorrupt},
		"tampered tag":      {key: key, envelope: flippedTag, want: errSecretEnvelopeCorrupt},
		"unknown version":   {key: key, envelope: wrongVersion, want: errSecretEnvelopeVersion},
		"truncated":         {key: key, envelope: envelope[:len(envelope)-1], want: errSecretEnvelopeCorrupt},
		"garbage":           {key: key, envelope: garbage, want: errSecretEnvelopeTruncated},
		"short master key":  {key: key[:16], envelope: envelope, want: errSecretKeySize},
		"oversize open key": {key: append(append([]byte(nil), key...), 0x00), envelope: envelope, want: errSecretKeySize},
	} {
		t.Run(name, func(t *testing.T) {
			plain, err := openSecret(tc.key, tc.envelope)
			if !errors.Is(err, tc.want) {
				t.Fatalf("openSecret error = %v, want %v", err, tc.want)
			}
			if plain != nil {
				t.Fatal("openSecret returned data alongside an error")
			}
		})
	}
}

func TestSealSecretRejectsBadMasterKeySizes(t *testing.T) {
	for name, key := range map[string][]byte{
		"nil":   nil,
		"short": testMasterKey(0x66)[:31],
		"long":  append(testMasterKey(0x77), 0x00),
	} {
		t.Run(name, func(t *testing.T) {
			envelope, err := sealSecret(key, []byte("secret-value"))
			if !errors.Is(err, errSecretKeySize) {
				t.Fatalf("sealSecret error = %v, want errSecretKeySize", err)
			}
			if envelope != nil {
				t.Fatal("sealSecret returned an envelope alongside an error")
			}
		})
	}
}

func TestSecretEnvelopeErrorsCarryNoSecretMaterial(t *testing.T) {
	key := testMasterKey(0x88)
	envelope, err := sealSecret(key, []byte("secret-value"))
	if err != nil {
		t.Fatalf("sealSecret: %v", err)
	}

	_, openErr := openSecret(testMasterKey(0x99), envelope)
	if openErr == nil {
		t.Fatal("openSecret accepted the wrong master key")
	}
	message := openErr.Error()
	if strings.Contains(message, "secret-value") || strings.Contains(message, string(key)) {
		t.Fatal("error message leaked secret material")
	}
}
