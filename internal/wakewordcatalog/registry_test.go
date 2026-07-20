package wakewordcatalog

import (
	"strings"
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

func TestByIDFindsBrandPhrase(t *testing.T) {
	m, ok := ByID("hey_kombify")
	if !ok {
		t.Fatal("hey_kombify not found in registry")
	}
	if m.WakeWord != "Hey Kombify" {
		t.Errorf("WakeWord = %q, want %q", m.WakeWord, "Hey Kombify")
	}
	if !m.HasOpenWakeWord() {
		t.Error("hey_kombify should ship a published openWakeWord model")
	}
	if got := m.OpenWakeWord.File.SHA256; got != "24c6d2d1c235892362ebf12b0055801d2f8461f856e15d704c3d8262304f4c9f" {
		t.Errorf("hey_kombify ONNX SHA256 = %q (unexpected drift from the published catalog)", got)
	}
	if m.OpenWakeWord.RecommendedThreshold != 0.55 {
		t.Errorf("hey_kombify recommended threshold = %v, want 0.55", m.OpenWakeWord.RecommendedThreshold)
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

func TestMicroWakeWordPendingUntilPublished(t *testing.T) {
	// microWakeWord TFLite models are not trained/published yet; every registry
	// entry must report the microWakeWord variant as unavailable so the
	// serving endpoint reports it as pending rather than 404-ing on a dangling
	// file. Flip this expectation deliberately when the dual-export training
	// publishes real .tflite artifacts.
	for _, m := range All() {
		if m.HasMicroWakeWord() {
			t.Errorf("model %q unexpectedly reports a published microWakeWord model; update the test when training lands", m.ID)
		}
	}
}

func TestSingleWordPhrases(t *testing.T) {
	// The Kombify-Box vision wants single-word, no-"Hey" wake phrases. Both are
	// registered so the model hub knows the IDs (answers pending, not 404), and
	// neither has an on-device microWakeWord (.tflite) variant published yet.
	for _, id := range []string{"jarvis", "kombify"} {
		m, ok := ByID(id)
		if !ok {
			t.Fatalf("single-word phrase %q missing from registry", id)
		}
		if strings.Contains(strings.ToLower(m.WakeWord), "hey") {
			t.Errorf("%q wake word %q must be a single word without \"Hey\"", id, m.WakeWord)
		}
		if m.HasMicroWakeWord() {
			t.Errorf("%q microWakeWord is not published yet; flip this test when training lands", id)
		}
	}

	// "kombify" ships a published single-word openWakeWord head; "jarvis" does
	// not yet (only the two-word "hey_jarvis" exists).
	kombify, _ := ByID("kombify")
	if !kombify.HasOpenWakeWord() {
		t.Error("kombify should ship the published single-word openWakeWord head (kombify.onnx)")
	}
	if got := kombify.OpenWakeWord.File.SHA256; got != "1cf8e8d80f2c9515fbcfbd36e99d537eb8fb87132657c03b6a4e65f606db2769" {
		t.Errorf("kombify.onnx SHA256 = %q (unexpected drift from the published model)", got)
	}
	if kombify.OpenWakeWord.RecommendedThreshold != 0.50 {
		t.Errorf("kombify recommended threshold = %v, want 0.50", kombify.OpenWakeWord.RecommendedThreshold)
	}
	if jarvis, _ := ByID("jarvis"); jarvis.HasOpenWakeWord() {
		t.Error("jarvis has no single-word model published yet; it must not report one")
	}
}

func TestSharedONNXFrontendsPresent(t *testing.T) {
	if !SharedMelspec.present() || !SharedEmbedding.present() {
		t.Fatal("shared openWakeWord melspec/embedding frontends must have URLs")
	}
}
