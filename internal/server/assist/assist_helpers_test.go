//go:build linux

package assist

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"github.com/kombifyio/SpeechKit/pkg/speechkit"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/stt"
	"math"
	"testing"
)

// ── fakes ───────────────────────────────────────────────────────────────────

type fakeProcessor struct {
	result      speechkit.AssistResult
	err         error
	lastCalled  bool
	lastTranscr string
	lastReq     speechkit.AssistRequest
}

func (f *fakeProcessor) Process(_ context.Context, req speechkit.AssistRequest) (speechkit.AssistResult, error) {
	f.lastCalled = true
	f.lastTranscr = req.Text
	f.lastReq = req
	if f.err != nil {
		return speechkit.AssistResult{}, f.err
	}
	return f.result, nil
}

type fakeTranscriber struct {
	result   *stt.Result
	err      error
	lastOpts stt.TranscribeOpts
}

func (f *fakeTranscriber) Route(_ context.Context, _ []byte, _ float64, opts stt.TranscribeOpts) (*stt.Result, error) {
	f.lastOpts = opts
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

func okAssistResult() speechkit.AssistResult {
	return speechkit.AssistResult{
		Text:      "Sure, here is a quick summary.",
		SpeakText: "Sure, here is a quick summary.",
		Action:    "respond",
		Locale:    "en",
		Surface:   speechkit.AssistSurfacePanel,
		Kind:      "answer",
		Audio:     speechkit.NewAudioData([]byte{0x00, 0x01, 0x02, 0x03}),
		Format:    "mp3",
	}
}

func mustHandler(t *testing.T, opts Options) *Handler {
	t.Helper()
	if opts.MaxUploadMB == 0 {
		opts.MaxUploadMB = 25
	}
	h, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}

func synthSine(rate, ms int) []byte {
	frames := rate * ms / 1000
	out := make([]byte, frames*2)
	for f := 0; f < frames; f++ {
		tSec := float64(f) / float64(rate)
		val := int16(math.Sin(2*math.Pi*440.0*tSec) * 8000)
		binary.LittleEndian.PutUint16(out[f*2:], uint16(val))
	}
	return out
}

func wrapWAV(pcm []byte, rate int) []byte {
	dataSize := uint32(len(pcm))
	buf := make([]byte, 44+len(pcm))
	copy(buf[0:], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], 36+dataSize)
	copy(buf[8:], "WAVE")
	copy(buf[12:], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], 1)
	binary.LittleEndian.PutUint16(buf[22:], 1)
	binary.LittleEndian.PutUint32(buf[24:], uint32(rate))
	binary.LittleEndian.PutUint32(buf[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(buf[32:], 2)
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], dataSize)
	copy(buf[44:], pcm)
	return buf
}
func base64Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}
