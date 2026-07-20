//go:build windows && cgo

// play-wav spielt eine 16-bit-PCM-WAV-Datei auf einem per Namens-Hint
// gewaehlten Wiedergabegeraet ab (z. B. dem Box-Speaker) — fuer automatische
// Wakeword-Loopback-Tests ohne menschlichen Sprecher. Ein Volume-Faktor
// skaliert die Samples, damit der Mikropfad nicht uebersteuert.
package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/malgo"
)

func main() {
	deviceHint := flag.String("device", "usb uac", "playback device name substring")
	wavPath := flag.String("wav", "", "16-bit PCM WAV to play")
	volume := flag.Float64("volume", 0.25, "linear volume scale, 0.0 to 1.0")
	flag.Parse()
	if *wavPath == "" {
		fatalf("usage: play-wav -wav file.wav [-device hint] [-volume 0.25]")
	}

	pcm, sampleRate, channels, err := readWAV(*wavPath)
	if err != nil {
		fatalf("read wav: %v", err)
	}
	scalePCM(pcm, *volume)

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		fatalf("init audio context: %v", err)
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	devices, err := ctx.Devices(malgo.Playback)
	if err != nil {
		fatalf("list playback devices: %v", err)
	}
	info, err := findDevice(devices, *deviceHint)
	if err != nil {
		fatalf("%v", err)
	}
	fmt.Printf("[play-wav] device=%q %d Hz/%dch/s16 volume=%.2f bytes=%d\n",
		info.Name(), sampleRate, channels, *volume, len(pcm))
	if err := playPCM(ctx, info, pcm, sampleRate, channels); err != nil {
		fatalf("play: %v", err)
	}
}

func readWAV(path string) ([]byte, uint32, uint32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, 0, err
	}
	if len(data) < 44 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, 0, 0, errors.New("not a RIFF/WAVE file")
	}
	var sampleRate, channels uint32
	pos := 12
	for pos+8 <= len(data) {
		id := string(data[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		body := pos + 8
		if body+size > len(data) {
			size = len(data) - body
		}
		switch id {
		case "fmt ":
			if size >= 16 {
				if binary.LittleEndian.Uint16(data[body:]) != 1 {
					return nil, 0, 0, errors.New("only PCM (format 1) supported")
				}
				channels = uint32(binary.LittleEndian.Uint16(data[body+2:]))
				sampleRate = binary.LittleEndian.Uint32(data[body+4:])
				if bits := binary.LittleEndian.Uint16(data[body+14:]); bits != 16 {
					return nil, 0, 0, fmt.Errorf("only 16-bit PCM supported, got %d", bits)
				}
			}
		case "data":
			if sampleRate == 0 || channels == 0 {
				return nil, 0, 0, errors.New("data chunk before fmt chunk")
			}
			return data[body : body+size], sampleRate, channels, nil
		}
		pos = body + size
		if size%2 == 1 {
			pos++
		}
	}
	return nil, 0, 0, errors.New("no data chunk found")
}

func scalePCM(pcm []byte, volume float64) {
	if volume < 0 {
		volume = 0
	}
	if volume > 1 {
		volume = 1
	}
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int16(binary.LittleEndian.Uint16(pcm[i:]))
		binary.LittleEndian.PutUint16(pcm[i:], uint16(int16(float64(v)*volume)))
	}
}

func findDevice(devices []malgo.DeviceInfo, hint string) (*malgo.DeviceInfo, error) {
	hint = strings.ToLower(strings.TrimSpace(hint))
	for i := range devices {
		if hint == "" || strings.Contains(strings.ToLower(devices[i].Name()), hint) {
			return &devices[i], nil
		}
	}
	names := make([]string, 0, len(devices))
	for _, d := range devices {
		names = append(names, d.Name())
	}
	return nil, fmt.Errorf("no playback device matching %q (available: %s)", hint, strings.Join(names, " | "))
}

func playPCM(ctx *malgo.AllocatedContext, info *malgo.DeviceInfo, pcm []byte, sampleRate, channels uint32) error {
	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatS16
	cfg.Playback.Channels = channels
	cfg.Playback.DeviceID = info.ID.Pointer()
	cfg.SampleRate = sampleRate

	var off int
	done := make(chan struct{})
	var once sync.Once
	onSend := func(out, _ []byte, frames uint32) {
		n := 0
		if off < len(pcm) {
			n = copy(out, pcm[off:])
			off += n
		}
		if n < len(out) {
			for i := n; i < len(out); i++ {
				out[i] = 0
			}
			once.Do(func() { close(done) })
		}
	}

	dev, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: onSend})
	if err != nil {
		return err
	}
	defer dev.Uninit()
	if err := dev.Start(); err != nil {
		return err
	}
	<-done
	time.Sleep(200 * time.Millisecond)
	return nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[play-wav] "+format+"\n", args...)
	os.Exit(1)
}
