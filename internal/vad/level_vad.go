package vad

import "math"

// LevelVAD is a tiny RMS-based voice activity detector that needs no ONNX
// runtime. It exists as a fallback for the dictation pipeline while
// [SileroVAD] is disabled due to the onnxruntime/sherpa-onnx ABI conflict
// (see silero.go for the full story).
//
// Speech detection is intentionally crude: compute the RMS of the frame,
// normalise to [0, 1] against the int16 full scale, and apply a piecewise
// mapping that produces 0 for noise-floor levels, 1 for clearly voiced
// audio, and a linear ramp in between. Consumers see the same float32
// probability the Silero model would have returned, so they can keep using
// the existing threshold (DictationSegmenter uses 0.5).
//
// Trade-offs:
//   - No frequency awareness — keyboard typing, fan noise, or paper rustle
//     above the threshold counts as speech and resets the silence timer.
//   - No hangover smoothing — short consonant gaps register as silence at
//     the frame level. The DictationSegmenter's pause threshold (default
//     700 ms) absorbs this in practice.
//
// The numbers are tuned against the desktop logs (`Overlay audio: raw=0.006`
// for room silence, `raw=0.012-0.026` for normal voice). The wider window
// here errs toward "speech" so the silence-cutoff timer waits for genuine
// quiet rather than tripping on a breath.
type LevelVAD struct {
	speechAbove  float64
	silenceBelow float64
}

// NewLevelVAD constructs a level-based detector with the production
// defaults. Tests can override the thresholds by mutating the struct
// directly — there is no setter API yet because the only caller is the
// dictation bootstrap.
func NewLevelVAD() *LevelVAD {
	return &LevelVAD{
		silenceBelow: 0.005,
		speechAbove:  0.020,
	}
}

// ProcessFrame implements [Detector] by returning a per-frame speech
// probability. The contract is the same as the Silero binding: 0 means
// silence, 1 means active speech, and the consumer compares against its
// own threshold.
func (v *LevelVAD) ProcessFrame(pcm []int16) (float32, error) {
	if v == nil || len(pcm) == 0 {
		return 0, nil
	}
	var sumSq float64
	for _, s := range pcm {
		x := float64(s)
		sumSq += x * x
	}
	rms := math.Sqrt(sumSq/float64(len(pcm))) / 32768.0
	if rms <= v.silenceBelow {
		return 0, nil
	}
	if rms >= v.speechAbove {
		return 1, nil
	}
	return float32((rms - v.silenceBelow) / (v.speechAbove - v.silenceBelow)), nil
}

// Reset is a no-op — the detector keeps no per-session state.
func (v *LevelVAD) Reset() {}
