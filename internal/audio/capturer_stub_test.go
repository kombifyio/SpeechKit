//go:build !windows || !cgo

package audio

import (
	"errors"
	"testing"
)

func TestNewCapturerWithoutNativeBackendReturnsUnavailableError(t *testing.T) {
	_, err := Open(Config{})
	if err == nil {
		t.Fatal("expected constructor error without native backend")
	}
	if !errors.Is(err, ErrBackendUnavailable) {
		t.Fatalf("expected ErrBackendUnavailable, got %v", err)
	}
}
