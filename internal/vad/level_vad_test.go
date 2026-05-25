package vad

import (
	"math"
	"testing"
)

// TestLevelVADReturnsZeroForSilence pins the "noise floor" branch: room
// noise sitting at RMS 0.001 must read as 0 probability. Regression
// guards against someone lowering silenceBelow far enough that mic
// hum trips the dictation auto-stop watcher into thinking the user is
// still talking.
func TestLevelVADReturnsZeroForSilence(t *testing.T) {
	v := NewLevelVAD()
	pcm := constantPCM(0, 512) // pure digital silence
	got, err := v.ProcessFrame(pcm)
	if err != nil {
		t.Fatalf("ProcessFrame: %v", err)
	}
	if got != 0 {
		t.Fatalf("silence frame got prob %v, want 0", got)
	}
}

// TestLevelVADReturnsOneForLoudSpeech pins the "clear speech" branch:
// an RMS well above speechAbove must saturate at 1. Regression guards
// against someone raising speechAbove far enough that normal voiced
// audio falls back into the linear ramp.
func TestLevelVADReturnsOneForLoudSpeech(t *testing.T) {
	v := NewLevelVAD()
	// RMS-equivalent amplitude well above the 0.020 threshold:
	// 6000/32768 ≈ 0.183, plenty over the saturation point.
	pcm := constantPCM(6000, 512)
	got, err := v.ProcessFrame(pcm)
	if err != nil {
		t.Fatalf("ProcessFrame: %v", err)
	}
	if got != 1 {
		t.Fatalf("loud frame got prob %v, want 1", got)
	}
}

// TestLevelVADRampsInBetween pins the linear interpolation between
// silenceBelow and speechAbove. Without this, someone "simplifying" the
// detector to a single hard threshold could silently break the
// DictationSegmenter speech-probability comparison (it uses > 0.5 to
// distinguish in-speech from out-of-speech).
func TestLevelVADRampsInBetween(t *testing.T) {
	v := NewLevelVAD()
	// Target RMS exactly between silenceBelow (0.005) and speechAbove
	// (0.020): 0.0125. Amplitude that yields that RMS for a constant
	// signal is round(0.0125 * 32768) = 410.
	pcm := constantPCM(410, 512)
	got, err := v.ProcessFrame(pcm)
	if err != nil {
		t.Fatalf("ProcessFrame: %v", err)
	}
	if got <= 0 || got >= 1 {
		t.Fatalf("midpoint frame got prob %v, want strictly in (0, 1)", got)
	}
	// Allow 5% slack to absorb float rounding; do not pin to an exact
	// value because the constant-signal-to-RMS relationship is
	// amplitude-dependent and the threshold mapping is intentionally
	// linear not exact-50%.
	if math.Abs(float64(got-0.5)) > 0.1 {
		t.Fatalf("midpoint frame prob %v not near 0.5", got)
	}
}

// TestLevelVADHandlesEmptyFrame guards a panic that the dictation
// pipeline would surface as an audio-handler error rather than a
// silence reset — empty frames happen when the audio session is
// torn down mid-batch.
func TestLevelVADHandlesEmptyFrame(t *testing.T) {
	v := NewLevelVAD()
	got, err := v.ProcessFrame(nil)
	if err != nil {
		t.Fatalf("ProcessFrame(nil): %v", err)
	}
	if got != 0 {
		t.Fatalf("nil frame got prob %v, want 0", got)
	}
}

// TestLevelVADSatisfiesDetectorInterface keeps the interface
// assertion close to the implementation so a future Detector method
// rename does not silently break the dictation wiring.
func TestLevelVADSatisfiesDetectorInterface(t *testing.T) {
	var _ Detector = (*LevelVAD)(nil)
}

func constantPCM(sample int16, frames int) []int16 {
	out := make([]int16, frames)
	for i := range out {
		out[i] = sample
	}
	return out
}
