//go:build windows && cgo

package main

import (
	"encoding/binary"
	"testing"
)

func TestNormalizePlaybackPCM16_ResamplesMonoToBoxFormat(t *testing.T) {
	in := make([]byte, 22050*2)
	for i := 0; i < 22050; i++ {
		binary.LittleEndian.PutUint16(in[i*2:], uint16(int16(i%2000-1000)))
	}

	out, rate, channels, err := normalizePlaybackPCM16(in, 22050, 1)
	if err != nil {
		t.Fatalf("normalizePlaybackPCM16: %v", err)
	}
	if rate != boxPlaybackSampleRate || channels != boxPlaybackChannels {
		t.Fatalf("format = %d Hz/%d ch, want %d Hz/%d ch", rate, channels, boxPlaybackSampleRate, boxPlaybackChannels)
	}
	if got, want := len(out), boxPlaybackSampleRate*boxPlaybackChannels*2; got != want {
		t.Fatalf("len(out) = %d, want %d", got, want)
	}
	left := binary.LittleEndian.Uint16(out[0:2])
	right := binary.LittleEndian.Uint16(out[2:4])
	if left != right {
		t.Fatalf("stereo channels differ: left=%d right=%d", left, right)
	}
}

func TestNormalizePlaybackPCM16_KeepsMatchingBoxFormat(t *testing.T) {
	in := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	out, rate, channels, err := normalizePlaybackPCM16(in, boxPlaybackSampleRate, boxPlaybackChannels)
	if err != nil {
		t.Fatalf("normalizePlaybackPCM16: %v", err)
	}
	if rate != boxPlaybackSampleRate || channels != boxPlaybackChannels {
		t.Fatalf("format = %d Hz/%d ch", rate, channels)
	}
	if len(out) != len(in) || &out[0] != &in[0] {
		t.Fatalf("matching box format should reuse input slice")
	}
}
