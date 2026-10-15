package models

import (
	"errors"
	"testing"
)

// A composition root that forgets to inject a builder must fail closed rather
// than report "no providers" or reach for a default provider set.
func TestBuildSTTFailsClosedWithoutBuilder(t *testing.T) {
	_, provider, err := BuildSTT(nil, STTBuildSpec{Provider: "openai"})
	if !errors.Is(err, ErrNoSTTBuilder) {
		t.Fatalf("err = %v, want ErrNoSTTBuilder", err)
	}
	if provider != nil {
		t.Fatalf("provider = %v, want nil", provider)
	}
}
