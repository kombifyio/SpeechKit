//go:build cgo

package engine

import (
	"errors"
	"strings"
	"sync"
	"unsafe"

	sherpaonnx "github.com/k2-fsa/sherpa-onnx-go/sherpa_onnx"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/x/stt/sherpa"
)

// sherpa-onnx-go returns an OfflineRecognizer even when the model fails to
// load, with a nil C handle as its only field; NewRecognizer reads that handle
// to tell. This declaration stops the build if the struct ever gains a field.
var _ [unsafe.Sizeof(sherpaonnx.OfflineRecognizer{}) - unsafe.Sizeof(uintptr(0))]struct{} = [0]struct{}{}

// decodeBatch bounds how many segments of one recording are decoded together;
// each in-flight segment holds its own encoder activations.
const decodeBatch = 4

// Recognizer is a loaded model. Its methods are safe for concurrent use;
// decodes run one recording at a time.
type Recognizer struct {
	mu  sync.Mutex
	rec *sherpaonnx.OfflineRecognizer
}

// NewRecognizer loads the model in cfg.ModelDir. Loading takes a few seconds
// and about 1 GB of memory for Parakeet TDT 0.6B int8.
func NewRecognizer(cfg sherpa.Config) (*Recognizer, error) {
	files, err := sherpa.ResolveModelDir(cfg.ModelDir)
	if err != nil {
		return nil, err
	}

	rc := sherpaonnx.OfflineRecognizerConfig{}
	// The feature dimension is read from the model's metadata (128 for
	// Parakeet); this value only applies to models without one.
	rc.FeatConfig = sherpaonnx.FeatureConfig{SampleRate: sherpa.SampleRate, FeatureDim: 80}
	rc.ModelConfig.Transducer = sherpaonnx.OfflineTransducerModelConfig{
		Encoder: nativePath(files.Encoder),
		Decoder: nativePath(files.Decoder),
		Joiner:  nativePath(files.Joiner),
	}
	rc.ModelConfig.Tokens = nativePath(files.Tokens)
	rc.ModelConfig.NumThreads = max(cfg.Threads, 1)
	rc.ModelConfig.Provider = "cpu"
	rc.ModelConfig.ModelType = "nemo_transducer"
	// Greedy search, not modified_beam_search: sherpa-onnx v1.13.8's NeMo
	// beam search ranks TDT hypotheses that sit at different frames by their
	// summed log-probability alone, so a hypothesis that skipped speech can
	// win and a whole sentence goes missing (a 7 s German/English clip lost
	// its second half). Greedy search keeps every word; it also means the
	// model takes no per-request hotwords, which only the beam search reads.
	rc.DecodingMethod = "greedy_search"

	rec := sherpaonnx.NewOfflineRecognizer(&rc)
	if rec == nil || *(*unsafe.Pointer)(unsafe.Pointer(rec)) == nil {
		return nil, errors.New("sherpa: the model failed to load; it is not a sherpa-onnx nemo_transducer export, a file is damaged, or its path has characters the runtime cannot open")
	}
	return &Recognizer{rec: rec}, nil
}

// Transcribe decodes 16 kHz mono samples.
func (r *Recognizer) Transcribe(samples []float32) (sherpa.Result, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rec == nil {
		return sherpa.Result{}, errors.New("sherpa: recognizer is closed")
	}
	result := sherpa.Result{Duration: float64(len(samples)) / sherpa.SampleRate}
	if len(samples) == 0 {
		return result, nil
	}
	spans := sherpa.SplitAtPauses(samples)
	texts := make([]string, 0, len(spans))
	for first := 0; first < len(spans); first += decodeBatch {
		batch := spans[first:min(first+decodeBatch, len(spans))]
		streams := make([]*sherpaonnx.OfflineStream, 0, len(batch))
		for _, span := range batch {
			stream := sherpaonnx.NewOfflineStream(r.rec)
			stream.AcceptWaveform(sherpa.SampleRate, samples[span.Start:span.End])
			streams = append(streams, stream)
		}
		r.rec.DecodeStreams(streams)
		for i, stream := range streams {
			decoded := stream.GetResult()
			text := strings.TrimSpace(decoded.Text)
			start := float64(batch[i].Start) / sherpa.SampleRate
			end := float64(batch[i].End) / sherpa.SampleRate
			result.Segments = append(result.Segments, sherpa.Segment{Start: start, End: end, Text: text})
			result.Words = append(result.Words, sherpa.WordsFromTokens(decoded.Tokens, decoded.Timestamps, start, end)...)
			if text != "" {
				texts = append(texts, text)
			}
		}
		for _, stream := range streams {
			sherpaonnx.DeleteOfflineStream(stream)
		}
	}
	result.Text = strings.Join(texts, " ")
	return result, nil
}

// Close releases the model. Transcribe fails afterwards.
func (r *Recognizer) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rec != nil {
		sherpaonnx.DeleteOfflineRecognizer(r.rec)
		r.rec = nil
	}
}
