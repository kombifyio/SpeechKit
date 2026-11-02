//go:build cgo

package engine

import (
	"errors"
	"sync"
	"unsafe"

	sherpaonnx "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/tts/sherpa"
)

// The Piper VITS exports SpeechKit ships were trained at these scales. They
// match the .onnx.json of both bundled voices.
const (
	noiseScale  = 0.667
	noiseScaleW = 0.8
	lengthScale = 1.0
)

// sherpa-onnx-go returns an OfflineTts even when the model fails to load, with
// a nil C handle as its only field. This declaration stops the build if the
// struct ever gains a field, so the nil check below stays valid.
var _ [unsafe.Sizeof(sherpaonnx.OfflineTts{}) - unsafe.Sizeof(uintptr(0))]struct{} = [0]struct{}{}

// voice is one loaded model.
type voice struct {
	id  string
	tts *sherpaonnx.OfflineTts
}

// Engine holds the German and English voices. Speak is safe for concurrent
// use; one synthesis runs at a time because the native runtime is not.
type Engine struct {
	mu      sync.Mutex
	german  voice
	english voice
}

// New loads both voices from the models\tts directory dir. threads is the CPU
// thread count per voice; values below 1 become 1.
func New(dir string, threads int) (*Engine, error) {
	voices, err := sherpa.ResolveVoices(dir)
	if err != nil {
		return nil, err
	}
	if threads < 1 {
		threads = 1
	}
	german, err := loadVoice(voices.German, threads)
	if err != nil {
		return nil, err
	}
	english, err := loadVoice(voices.English, threads)
	if err != nil {
		sherpaonnx.DeleteOfflineTts(german.tts)
		return nil, err
	}
	return &Engine{german: german, english: english}, nil
}

func loadVoice(v sherpa.Voice, threads int) (voice, error) {
	cfg := sherpaonnx.OfflineTtsConfig{}
	cfg.Model.Vits = sherpaonnx.OfflineTtsVitsModelConfig{
		Model:       nativePath(v.Model),
		Tokens:      nativePath(v.Tokens),
		DataDir:     nativePath(v.DataDir),
		NoiseScale:  noiseScale,
		NoiseScaleW: noiseScaleW,
		LengthScale: lengthScale,
	}
	cfg.Model.NumThreads = threads
	cfg.Model.Provider = "cpu"
	tts := sherpaonnx.NewOfflineTts(&cfg)
	if tts == nil || *(*unsafe.Pointer)(unsafe.Pointer(tts)) == nil {
		return voice{}, errors.New("sherpa tts: " + v.ID + " failed to load; the ONNX file is damaged, espeak-ng-data is incomplete, or a path has characters the runtime cannot open")
	}
	return voice{id: v.ID, tts: tts}, nil
}

// Speak synthesizes text with the voice locale selects. speed is a multiplier
// (1 keeps the trained pace); zero or negative means 1. The returned samples
// are mono float32 in [-1, 1].
func (e *Engine) Speak(text, locale string, speed float32) ([]float32, int, string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	chosen := e.english
	if sherpa.IsGermanLocale(locale) {
		chosen = e.german
	}
	if chosen.tts == nil {
		return nil, 0, "", errors.New("sherpa tts: engine is closed")
	}
	if speed <= 0 {
		speed = 1
	}
	// sherpa's speed is the inverse of Piper's length scale: larger is faster.
	audio := chosen.tts.Generate(text, 0, speed)
	if audio == nil || len(audio.Samples) == 0 {
		return nil, 0, "", errors.New("sherpa tts: " + chosen.id + " produced no audio")
	}
	samples := make([]float32, len(audio.Samples))
	copy(samples, audio.Samples)
	rate := audio.SampleRate
	if rate <= 0 {
		rate = sherpa.WAVSampleRate
	}
	return samples, rate, chosen.id, nil
}

// Close releases both models. Speak fails afterwards.
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.german.tts != nil {
		sherpaonnx.DeleteOfflineTts(e.german.tts)
		e.german.tts = nil
	}
	if e.english.tts != nil {
		sherpaonnx.DeleteOfflineTts(e.english.tts)
		e.english.tts = nil
	}
}

// Compile-time check that Engine is a sherpa.Speaker.
var _ sherpa.Speaker = (*Engine)(nil)
