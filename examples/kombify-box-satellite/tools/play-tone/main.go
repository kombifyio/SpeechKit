//go:build windows && cgo

package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/malgo"
)

const (
	sampleRate = 48000
	channels   = 2
)

func main() {
	deviceHint := flag.String("device", "usb uac", "playback device name substring")
	frequency := flag.Float64("freq", 880, "tone frequency in Hz")
	seconds := flag.Float64("seconds", 2.0, "tone duration in seconds")
	volume := flag.Float64("volume", 0.20, "tone volume, 0.0 to 1.0")
	list := flag.Bool("list", false, "list playback devices and exit")
	flag.Parse()

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
	if *list {
		for _, d := range devices {
			fmt.Println(d.Name())
		}
		return
	}

	info, err := findDevice(devices, *deviceHint)
	if err != nil {
		fatalf("%v", err)
	}
	pcm := makeTone(*frequency, *seconds, *volume)
	fmt.Printf("[play-tone] device=%q format=%d Hz/%dch/s16 freq=%.1f Hz duration=%.2fs bytes=%d\n",
		info.Name(), sampleRate, channels, *frequency, *seconds, len(pcm))

	if err := playPCM(ctx, info, pcm); err != nil {
		fatalf("play tone: %v", err)
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

func makeTone(freq, seconds, volume float64) []byte {
	if seconds <= 0 {
		seconds = 1
	}
	if volume < 0 {
		volume = 0
	}
	if volume > 1 {
		volume = 1
	}
	frames := int(math.Ceil(seconds * sampleRate))
	pcm := make([]byte, frames*channels*2)
	amp := volume * float64(math.MaxInt16)
	fadeFrames := sampleRate / 50 // 20 ms
	if fadeFrames > frames/2 {
		fadeFrames = frames / 2
	}
	for i := 0; i < frames; i++ {
		env := 1.0
		if fadeFrames > 0 {
			if i < fadeFrames {
				env = float64(i) / float64(fadeFrames)
			} else if remain := frames - 1 - i; remain < fadeFrames {
				env = float64(remain) / float64(fadeFrames)
			}
		}
		v := int16(math.Round(amp * env * math.Sin(2*math.Pi*freq*float64(i)/sampleRate)))
		off := i * channels * 2
		binary.LittleEndian.PutUint16(pcm[off:], uint16(v))
		binary.LittleEndian.PutUint16(pcm[off+2:], uint16(v))
	}
	return pcm
}

func playPCM(ctx *malgo.AllocatedContext, info *malgo.DeviceInfo, pcm []byte) error {
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
	fmt.Fprintf(os.Stderr, "[play-tone] "+format+"\n", args...)
	os.Exit(1)
}
