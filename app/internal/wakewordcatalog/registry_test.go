package wakewordcatalog

import (
	"testing"
)

func TestAllReturnsIndependentCopy(t *testing.T) {
	a := All()
	if len(a) == 0 {
		t.Fatal("registry is empty")
	}
	// Mutating the returned slice must not affect the shared registry.
	a[0].WakeWord = "MUTATED"
	b := All()
	if b[0].WakeWord == "MUTATED" {
		t.Fatal("All() leaked a reference to the shared registry backing array")
	}
}

func TestByIDAliasesHeyQubyToHeyKubi(t *testing.T) {
	canonical, ok := ByID("hey_kubi")
	if !ok {
		t.Fatal("hey_kubi not found in registry")
	}
	if canonical.ID != "hey_kubi" {
		t.Fatalf("canonical id = %q, want hey_kubi", canonical.ID)
	}
	aliased, ok := ByID("hey_quby")
	if !ok {
		t.Fatal("hey_quby alias must still resolve")
	}
	if aliased.ID != "hey_kubi" {
		t.Fatalf("hey_quby alias id = %q, want hey_kubi", aliased.ID)
	}
	if aliased.OpenWakeWord.File.SHA256 != canonical.OpenWakeWord.File.SHA256 {
		t.Fatal("hey_quby alias must point at the same published ONNX")
	}
}

func TestByIDIsCaseInsensitiveAndMissReturnsFalse(t *testing.T) {
	if _, ok := ByID("HEY_KOMBIFY"); !ok {
		t.Error("ByID should be case-insensitive")
	}
	if _, ok := ByID("does_not_exist"); ok {
		t.Error("ByID should return false for an unknown id")
	}
}
