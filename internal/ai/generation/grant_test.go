package generation

import (
	"context"
	"testing"
)

func TestGrantedRefusesUngrantedPurposesBeforeTheProvider(t *testing.T) {
	inner := &scriptedGenerator{provider: "foundry", models: []Model{{ID: "foundry/gpt-5.6-luna", Provider: "foundry"}}}
	granted := false
	gate := NewGranted(inner, "foundry", func(purpose Purpose) bool {
		return purpose != PurposeMeetingSynthesis || granted
	})

	_, err := gate.Generate(context.Background(), Request{Purpose: PurposeMeetingSynthesis})
	if Kind(err) != ErrorConsent {
		t.Fatalf("kind = %q, want consent", Kind(err))
	}
	if inner.calls != 0 {
		t.Fatal("the provider was contacted without permission")
	}
	catalog, _ := gate.Models(context.Background(), ModelQuery{Purpose: PurposeMeetingSynthesis})
	if len(catalog.Models) != 1 {
		t.Fatal("a gated provider must stay listed so callers can name the missing permission")
	}

	if _, err := gate.Generate(context.Background(), Request{Purpose: PurposeAssist}); err != nil {
		t.Fatalf("assist is not gated: %v", err)
	}
	granted = true
	if _, err := gate.Generate(context.Background(), Request{Purpose: PurposeMeetingSynthesis}); err != nil {
		t.Fatalf("with the grant: %v", err)
	}
	if gate.ProviderID() != "foundry" {
		t.Fatalf("provider id = %q", gate.ProviderID())
	}
}
