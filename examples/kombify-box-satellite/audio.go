//go:build windows && cgo

package main

// Audio I/O for the kombify box USB audio device (any OS malgo supports;
// device selection is by name substring, e.g. "kombify box").

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/malgo"
)

const (
	boxPlaybackSampleRate = 48000
	boxPlaybackChannels   = 2
)

type AudioIO struct {
	ctx        *malgo.AllocatedContext
	inputHint  string
	outputHint string

	fanout    audioFanout
	capture   *malgo.Device
	captureOn bool
}

func NewAudioIO(inputHint, outputHint string) (*AudioIO, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, fmt.Errorf("audio: init context: %w", err)
	}
	return &AudioIO{ctx: ctx, inputHint: strings.ToLower(inputHint), outputHint: strings.ToLower(outputHint)}, nil
}

func (a *AudioIO) findDevice(kind malgo.DeviceType, hint string) (*malgo.DeviceInfo, error) {
	infos, err := a.ctx.Devices(kind)
	if err != nil {
		return nil, err
	}
	for i := range infos {
		if hint == "" || strings.Contains(strings.ToLower(infos[i].Name()), hint) {
			d := infos[i]
			return &d, nil
		}
	}
	names := make([]string, 0, len(infos))
	for i := range infos {
		names = append(names, infos[i].Name())
	}
	return nil, fmt.Errorf("audio: no device matching %q (available: %s)", hint, strings.Join(names, " | "))
}

// Subscribe returns a channel that receives 16 kHz mono S16LE PCM chunks.
func (a *AudioIO) Subscribe(buf int) chan []byte {
	return a.SubscribeMonitored(buf).frames
}

// SubscribeMonitored returns the frame stream and its exact local overflow
// counter. Callers that authorize side effects must require zero drops.
func (a *AudioIO) SubscribeMonitored(buf int) *audioSubscription {
	return a.fanout.subscribe(buf)
}

func (a *AudioIO) Unsubscribe(ch chan []byte) {
	a.fanout.unsubscribe(ch)
}

// StartCapture opens the box microphone at 16 kHz mono S16LE and fans frames
// out to all subscribers (wake-word pipeline, utterance recorder).
func (a *AudioIO) StartCapture() error {
	info, err := a.findDevice(malgo.Capture, a.inputHint)
	if err != nil {
		return err
	}
	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = 1
	cfg.SampleRate = 16000
	cfg.Capture.DeviceID = info.ID.Pointer()
	cfg.Alsa.NoMMap = 1

	onRecv := func(_, in []byte, frames uint32) {
		if len(in) == 0 {
			return
		}
		a.fanout.publish(in)
	}
	dev, err := malgo.InitDevice(a.ctx.Context, cfg, malgo.DeviceCallbacks{Data: onRecv})
	if err != nil {
		return fmt.Errorf("audio: init capture: %w", err)
	}
	if err := dev.Start(); err != nil {
		return fmt.Errorf("audio: start capture: %w", err)
	}
	a.capture = dev
	a.captureOn = true
	fmt.Printf("[audio] capturing from %q @16k mono\n", info.Name())
	return nil
}

// PlayPCM plays S16LE PCM on the box speaker, blocking until playback finishes.
// The ESP32 firmware exposes UAC playback at 48 kHz stereo and writes that
// stream straight into I2S, so normalize every provider output to that shape.
func (a *AudioIO) PlayPCM(pcm []byte, sampleRate int, channels int) error {
	if len(pcm) == 0 {
		return nil
	}
	a.fanout.beginPlayback()
	defer a.fanout.endPlayback()
	var err error
	pcm, sampleRate, channels, err = normalizePlaybackPCM16(pcm, sampleRate, channels)
	if err != nil {
		return err
	}
	info, err := a.findDevice(malgo.Playback, a.outputHint)
	if err != nil {
		return err
	}
	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatS16
	cfg.Playback.Channels = uint32(channels)
	cfg.SampleRate = uint32(sampleRate)
	cfg.Playback.DeviceID = info.ID.Pointer()

	var off int
	done := make(chan struct{})
	var once sync.Once
	onSend := func(out, _ []byte, frames uint32) {
		n := copy(out, pcm[off:])
		off += n
		if n < len(out) {
			for i := n; i < len(out); i++ {
				out[i] = 0
			}
			once.Do(func() { close(done) })
		}
	}
	dev, err := malgo.InitDevice(a.ctx.Context, cfg, malgo.DeviceCallbacks{Data: onSend})
	if err != nil {
		return fmt.Errorf("audio: init playback: %w", err)
	}
	defer dev.Uninit()
	if err := dev.Start(); err != nil {
		return fmt.Errorf("audio: start playback: %w", err)
	}
	select {
	case <-done:
		time.Sleep(150 * time.Millisecond) // drain tail
	case <-time.After(2 * time.Minute):
	}
	return nil
}

type cueTone struct {
	freqHz int
	ms     int
	gain   float64
	gapMs  int
}

// Ding plays a short confirmation tone (wake acknowledged).
func (a *AudioIO) Ding() {
	_ = a.PlayCue("wake")
}

// CaptureAccepted plays a subtle two-note confirmation after user audio was
// accepted and the server turn is being committed.
func (a *AudioIO) CaptureAccepted() {
	_ = a.PlayCue("accepted")
}

func (a *AudioIO) PlayCue(name string) error {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "accepted", "captured", "understood":
		return a.playCueTones([]cueTone{
			{freqHz: 660, ms: 70, gain: 0.22, gapMs: 22},
			{freqHz: 990, ms: 95, gain: 0.20, gapMs: 0},
		})
	case "error":
		return a.playCueTones([]cueTone{
			{freqHz: 260, ms: 130, gain: 0.22, gapMs: 35},
			{freqHz: 180, ms: 170, gain: 0.20, gapMs: 0},
		})
	case "done", "answer":
		// Endton: Verarbeitung fertig, jetzt kommt die Antwort. Bewusst
		// weicher/kuerzer als "accepted", damit er die Antwort nur ankuendigt.
		return a.playCueTones([]cueTone{
			{freqHz: 520, ms: 55, gain: 0.16, gapMs: 18},
			{freqHz: 780, ms: 70, gain: 0.15, gapMs: 60},
		})
	case "wake", "":
		return a.playCueTones([]cueTone{
			{freqHz: 880, ms: 160, gain: 0.24, gapMs: 0},
		})
	default:
		return fmt.Errorf("audio: unknown cue %q", name)
	}
}

func (a *AudioIO) playCueTones(tones []cueTone) error {
	const rate = 24000
	var pcm []byte
	for _, tone := range tones {
		if tone.ms <= 0 || tone.freqHz <= 0 {
			continue
		}
		gain := tone.gain
		if gain <= 0 {
			gain = 0.2
		}
		if gain > 1 {
			gain = 1
		}
		n := rate * tone.ms / 1000
		start := len(pcm)
		pcm = append(pcm, make([]byte, n*2)...)
		for i := 0; i < n; i++ {
			t := float64(i) / rate
			env := cueEnvelope(i, n)
			v := int16(float64(math.MaxInt16) * gain * env * math.Sin(2*math.Pi*float64(tone.freqHz)*t))
			binary.LittleEndian.PutUint16(pcm[start+i*2:], uint16(v))
		}
		if tone.gapMs > 0 {
			pcm = append(pcm, make([]byte, rate*tone.gapMs/1000*2)...)
		}
	}
	return a.PlayPCM(pcm, rate, 1)
}

func cueEnvelope(i, n int) float64 {
	if n <= 1 {
		return 0
	}
	pos := float64(i) / float64(n-1)
	const edge = 0.18
	switch {
	case pos < edge:
		return pos / edge
	case pos > 1-edge:
		return (1 - pos) / edge
	default:
		return 1
	}
}

func normalizePlaybackPCM16(pcm []byte, sampleRate int, channels int) ([]byte, int, int, error) {
	if len(pcm) == 0 {
		return nil, boxPlaybackSampleRate, boxPlaybackChannels, nil
	}
	if sampleRate <= 0 {
		return nil, 0, 0, fmt.Errorf("audio: invalid sample rate %d", sampleRate)
	}
	if channels <= 0 {
		return nil, 0, 0, fmt.Errorf("audio: invalid channel count %d", channels)
	}
	bytesPerFrame := channels * 2
	inFrames := len(pcm) / bytesPerFrame
	if inFrames == 0 {
		return nil, 0, 0, fmt.Errorf("audio: PCM too short for %d channels", channels)
	}
	pcm = pcm[:inFrames*bytesPerFrame]
	if sampleRate == boxPlaybackSampleRate && channels == boxPlaybackChannels {
		return pcm, sampleRate, channels, nil
	}

	outFrames := int(math.Ceil(float64(inFrames) * float64(boxPlaybackSampleRate) / float64(sampleRate)))
	if outFrames < 1 {
		outFrames = 1
	}
	out := make([]byte, outFrames*boxPlaybackChannels*2)
	ratio := float64(sampleRate) / float64(boxPlaybackSampleRate)
	for i := 0; i < outFrames; i++ {
		srcPos := float64(i) * ratio
		idx := int(srcPos)
		frac := srcPos - float64(idx)
		if idx >= inFrames-1 {
			idx = inFrames - 1
			frac = 0
		}
		a := monoSampleAtPCM16(pcm, idx, channels)
		b := a
		if idx+1 < inFrames {
			b = monoSampleAtPCM16(pcm, idx+1, channels)
		}
		v := clampPCM16(float64(a)*(1-frac) + float64(b)*frac)
		off := i * boxPlaybackChannels * 2
		binary.LittleEndian.PutUint16(out[off:], uint16(v))
		binary.LittleEndian.PutUint16(out[off+2:], uint16(v))
	}
	return out, boxPlaybackSampleRate, boxPlaybackChannels, nil
}

func monoSampleAtPCM16(pcm []byte, frame int, channels int) int16 {
	if channels <= 1 {
		off := frame * 2
		return int16(binary.LittleEndian.Uint16(pcm[off:]))
	}
	base := frame * channels * 2
	var sum int
	for ch := 0; ch < channels; ch++ {
		off := base + ch*2
		sum += int(int16(binary.LittleEndian.Uint16(pcm[off:])))
	}
	return int16(sum / channels)
}

func clampPCM16(v float64) int16 {
	if v > math.MaxInt16 {
		return math.MaxInt16
	}
	if v < math.MinInt16 {
		return math.MinInt16
	}
	return int16(math.Round(v))
}

// normalizeResponseGain hebt leise Agent-Antworten auf einen einheitlichen
// Abhoerpegel: Peak-Normalisierung Richtung ~-1.5 dBFS, Verstaerkung auf 4x
// begrenzt, nie abschwaechen. Aura-TTS liegt deutlich unter den Cue-Toenen -
// ohne Anhebung klingt die Antwort leise und der Amp-Grundrausch faellt auf.
func normalizeResponseGain(pcm []byte) []byte {
	var peak int
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int(int16(binary.LittleEndian.Uint16(pcm[i:])))
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
	}
	if peak == 0 {
		return pcm
	}
	gain := 27500.0 / float64(peak)
	if gain <= 1 {
		return pcm
	}
	if gain > 4 {
		gain = 4
	}
	out := make([]byte, len(pcm)&^1)
	for i := 0; i+1 < len(pcm); i += 2 {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[i:]))) * gain
		binary.LittleEndian.PutUint16(out[i:], uint16(clampPCM16(v)))
	}
	return out
}
