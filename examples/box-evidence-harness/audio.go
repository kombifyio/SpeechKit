package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strings"
)

// Wire-contract rates (internal/server/voiceagent/protocol.go): the client
// streams 16 kHz PCM and the server returns 24 kHz PCM, both S16LE mono.
const (
	wsUplinkRate   = 16000
	wsDownlinkRate = 24000
)

// downlinkCandidates are the plausible server output rates the cross-check
// weighs the response byte count against.
var downlinkCandidates = []int{wsUplinkRate, wsDownlinkRate, 48000}

// loadUtterance reads a canonical PCM WAV and returns its sample rate and raw
// S16LE sample bytes (the `data` chunk payload). It tolerates auxiliary chunks
// (LIST/fact/etc.) between `fmt ` and `data`, which the checked-in fixtures
// carry.
func loadUtterance(path string) (rate int, pcm []byte, err error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- evidence fixture path is a trusted CLI flag, read-only.
	if err != nil {
		return 0, nil, err
	}
	return parseWAV(raw)
}

// parseWAV extracts the sample rate and `data` bytes from a RIFF/WAVE PCM
// container. It validates PCM (format 1), mono, 16-bit — the only shape the WS
// uplink accepts — so a mis-encoded fixture fails loudly instead of streaming
// garbage.
func parseWAV(raw []byte) (rate int, pcm []byte, err error) {
	if len(raw) < 12 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return 0, nil, errors.New("not a RIFF/WAVE file")
	}
	var (
		haveFmt  bool
		haveData bool
		channels uint16
		bits     uint16
	)
	off := 12
	for off+8 <= len(raw) {
		id := string(raw[off : off+4])
		size := int(binary.LittleEndian.Uint32(raw[off+4 : off+8]))
		body := off + 8
		if size < 0 || body+size > len(raw) {
			return 0, nil, fmt.Errorf("chunk %q claims %d bytes past end of file", id, size)
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return 0, nil, fmt.Errorf("fmt chunk too small (%d bytes)", size)
			}
			format := binary.LittleEndian.Uint16(raw[body : body+2])
			channels = binary.LittleEndian.Uint16(raw[body+2 : body+4])
			rate = int(binary.LittleEndian.Uint32(raw[body+4 : body+8]))
			bits = binary.LittleEndian.Uint16(raw[body+14 : body+16])
			if format != 1 {
				return 0, nil, fmt.Errorf("unsupported WAV format %d (need PCM=1)", format)
			}
			haveFmt = true
		case "data":
			pcm = raw[body : body+size]
			haveData = true
		}
		// Chunks are word-aligned: an odd size carries a pad byte.
		off = body + size
		if size%2 == 1 {
			off++
		}
		if haveFmt && haveData {
			break
		}
	}
	if !haveFmt {
		return 0, nil, errors.New("missing fmt chunk")
	}
	if !haveData {
		return 0, nil, errors.New("missing data chunk")
	}
	if channels != 1 {
		return 0, nil, fmt.Errorf("utterance has %d channels; need mono", channels)
	}
	if bits != 16 {
		return 0, nil, fmt.Errorf("utterance is %d-bit; need 16-bit S16LE", bits)
	}
	return rate, pcm, nil
}

// inferDownlinkRate picks the candidate rate whose implied playback duration
// best matches the ground-truth duration estimated from the output transcript
// (~2.5 synthesized words/sec). It returns the best-matching rate and the
// implied duration (seconds) for every candidate. When words == 0 the estimate
// is unavailable and the caller falls back to a plausibility band.
func inferDownlinkRate(responseBytes, words int) (best int, durations map[int]float64) {
	durations = make(map[int]float64, len(downlinkCandidates))
	for _, rate := range downlinkCandidates {
		durations[rate] = float64(responseBytes) / float64(bytesPerSecond(rate))
	}
	best = wsDownlinkRate
	if words < 2 {
		return best, durations
	}
	const wordsPerSecond = 2.5
	expected := float64(words) / wordsPerSecond
	bestDelta := -1.0
	for _, rate := range downlinkCandidates {
		delta := abs(durations[rate] - expected)
		if bestDelta < 0 || delta < bestDelta {
			bestDelta = delta
			best = rate
		}
	}
	return best, durations
}

// bytesPerSecond is the S16LE-mono byte rate for a sample rate.
func bytesPerSecond(rate int) int { return rate * 2 }

func countWords(s string) int { return len(strings.Fields(s)) }

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
