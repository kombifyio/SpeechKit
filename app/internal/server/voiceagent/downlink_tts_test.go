//go:build linux

package voiceagent

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/tts"
)

type fixedTTS struct{ result *tts.Result }

func (f fixedTTS) Synthesize(context.Context, string, tts.SynthesizeOpts) (*tts.Result, error) {
	return f.result, nil
}

// Clients play every downlink frame as 24 kHz S16LE mono, so synthesized
// speech reaches the wire in exactly that shape or the turn fails.
func TestDownlinkTTSDeliversWirePCM(t *testing.T) {
	// 100 ms of a 16 kHz stereo WAV becomes 100 ms of 24 kHz mono PCM.
	const sourceRate, channels, frames = 16000, 2, 1600
	data := make([]byte, frames*channels*2)
	for i := 0; i < frames*channels; i++ {
		binary.LittleEndian.PutUint16(data[i*2:], uint16(int16(i%200*100)))
	}
	wav := make([]byte, 44, 44+len(data))
	copy(wav[0:], "RIFF")
	binary.LittleEndian.PutUint32(wav[4:], uint32(36+len(data)))
	copy(wav[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(wav[16:], 16)
	binary.LittleEndian.PutUint16(wav[20:], 1)
	binary.LittleEndian.PutUint16(wav[22:], channels)
	binary.LittleEndian.PutUint32(wav[24:], sourceRate)
	binary.LittleEndian.PutUint32(wav[28:], sourceRate*channels*2)
	binary.LittleEndian.PutUint16(wav[32:], channels*2)
	binary.LittleEndian.PutUint16(wav[34:], 16)
	copy(wav[36:], "data")
	binary.LittleEndian.PutUint32(wav[40:], uint32(len(data)))
	wav = append(wav, data...)

	got, err := NewDownlinkTTS(fixedTTS{&tts.Result{Audio: wav, Format: "wav", SampleRate: sourceRate}}).
		Synthesize(context.Background(), "hello", tts.SynthesizeOpts{})
	if err != nil {
		t.Fatalf("Synthesize() error = %v", err)
	}
	if got.Format != "pcm" || got.SampleRate != DownlinkSampleRate {
		t.Fatalf("downlink = %s @ %d Hz, want pcm @ %d Hz", got.Format, got.SampleRate, DownlinkSampleRate)
	}
	if samples := len(got.Audio) / 2; samples < 2390 || samples > 2410 {
		t.Fatalf("downlink holds %d samples, want ~2400 (100 ms at 24 kHz)", samples)
	}

	if _, err := NewDownlinkTTS(fixedTTS{&tts.Result{Audio: []byte("OggS"), Format: "opus"}}).
		Synthesize(context.Background(), "hello", tts.SynthesizeOpts{}); err == nil {
		t.Fatal("an unconvertible payload reached the PCM wire")
	}
}
