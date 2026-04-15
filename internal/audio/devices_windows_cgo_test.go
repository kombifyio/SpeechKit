//go:build windows && cgo

package audio

import "testing"

func TestDeviceIDFromHexStringRoundTrip(t *testing.T) {
	id, ok, err := deviceIDFromHexString("010203")
	if err != nil {
		t.Fatalf("deviceIDFromHexString() error = %v", err)
	}
	if !ok {
		t.Fatal("deviceIDFromHexString() returned ok=false")
	}
	if got := id.String(); got != "010203" {
		t.Fatalf("round trip = %q, want %q", got, "010203")
	}
}

func TestDeviceIDFromHexStringRejectsInvalidInput(t *testing.T) {
	if _, ok, err := deviceIDFromHexString("zz"); err == nil || ok {
		t.Fatalf("expected parse failure, got ok=%v err=%v", ok, err)
	}
}
