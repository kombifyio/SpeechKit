//go:build windows && cgo

package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gen2brain/malgo"
)

const (
	sampleRate = 16000
	channels   = 1
)

func main() {
	deviceHint := flag.String("device", "usb uac", "capture device name substring")
	seconds := flag.Float64("seconds", 5, "capture duration in seconds")
	outPath := flag.String("out", "mic-capture.wav", "output WAV path")
	list := flag.Bool("list", false, "list capture devices and exit")
	flag.Parse()

	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		fatalf("init audio context: %v", err)
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()

	devices, err := ctx.Devices(malgo.Capture)
	if err != nil {
		fatalf("list capture devices: %v", err)
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

	pcm, err := recordPCM(ctx, info, *seconds)
	if err != nil {
		fatalf("record: %v", err)
	}
	if err := writeWAV(*outPath, pcm); err != nil {
		fatalf("write WAV: %v", err)
	}
	fmt.Printf("[record-mic] device=%q format=%d Hz/%dch/s16 seconds=%.2f bytes=%d out=%s\n",
		info.Name(), sampleRate, channels, *seconds, len(pcm), *outPath)
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
	return nil, fmt.Errorf("no capture device matching %q (available: %s)", hint, strings.Join(names, " | "))
}

func recordPCM(ctx *malgo.AllocatedContext, info *malgo.DeviceInfo, seconds float64) ([]byte, error) {
	if seconds <= 0 {
		seconds = 3
	}
	wantBytes := int(seconds * sampleRate * channels * 2)
	if wantBytes < sampleRate {
		wantBytes = sampleRate
	}
	buf := make([]byte, 0, wantBytes+sampleRate)
	done := make(chan struct{})
	var once sync.Once

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatS16
	cfg.Capture.Channels = channels
	cfg.Capture.DeviceID = info.ID.Pointer()
	cfg.SampleRate = sampleRate
	cfg.Alsa.NoMMap = 1

	onRecv := func(_, in []byte, frames uint32) {
		if len(in) == 0 {
			return
		}
		remaining := wantBytes - len(buf)
		if remaining <= 0 {
			once.Do(func() { close(done) })
			return
		}
		if len(in) > remaining {
			in = in[:remaining]
		}
		buf = append(buf, in...)
		if len(buf) >= wantBytes {
			once.Do(func() { close(done) })
		}
	}

	dev, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: onRecv})
	if err != nil {
		return nil, err
	}
	defer dev.Uninit()
	if err := dev.Start(); err != nil {
		return nil, err
	}
	select {
	case <-done:
	case <-time.After(time.Duration(seconds*float64(time.Second)) + 2*time.Second):
	}
	return buf, nil
}

func writeWAV(path string, pcm []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	dataLen := uint32(len(pcm))
	byteRate := uint32(sampleRate * channels * 2)
	blockAlign := uint16(channels * 2)
	header := make([]byte, 44)
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 36+dataLen)
	copy(header[8:12], "WAVE")
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1)
	binary.LittleEndian.PutUint16(header[22:24], channels)
	binary.LittleEndian.PutUint32(header[24:28], sampleRate)
	binary.LittleEndian.PutUint32(header[28:32], byteRate)
	binary.LittleEndian.PutUint16(header[32:34], blockAlign)
	binary.LittleEndian.PutUint16(header[34:36], 16)
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], dataLen)
	if _, err := f.Write(header); err != nil {
		return err
	}
	_, err = f.Write(pcm)
	return err
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[record-mic] "+format+"\n", args...)
	os.Exit(1)
}
