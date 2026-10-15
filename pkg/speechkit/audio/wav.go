package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// ParseWAV returns the S16LE data chunk of a PCM WAV file with its native
// sample rate and channel count.
func ParseWAV(raw []byte) (pcm []byte, sampleRate, channels int, err error) {
	if len(raw) < 44 || !bytes.Equal(raw[0:4], []byte("RIFF")) || !bytes.Equal(raw[8:12], []byte("WAVE")) {
		return nil, 0, 0, errors.New("audio: invalid WAV header")
	}

	// Walk chunks starting at offset 12. We need the fmt chunk and the data chunk.
	var (
		haveFmt     bool
		haveData    bool
		audioFormat uint16
		wavChannels uint16
		wavRate     uint32
		bps         uint16
		dataStart   int
		dataEnd     int
	)
	offset := 12
	for offset+8 <= len(raw) {
		id := string(raw[offset : offset+4])
		size := int(binary.LittleEndian.Uint32(raw[offset+4 : offset+8]))
		chunkStart := offset + 8
		chunkEnd := chunkStart + size
		if chunkEnd > len(raw) {
			// Some WAV files over-report the data chunk size; clamp to payload.
			chunkEnd = len(raw)
		}
		switch id {
		case "fmt ":
			if size < 16 || chunkEnd-chunkStart < 16 {
				return nil, 0, 0, errors.New("audio: malformed WAV fmt chunk")
			}
			audioFormat = binary.LittleEndian.Uint16(raw[chunkStart : chunkStart+2])
			wavChannels = binary.LittleEndian.Uint16(raw[chunkStart+2 : chunkStart+4])
			wavRate = binary.LittleEndian.Uint32(raw[chunkStart+4 : chunkStart+8])
			bps = binary.LittleEndian.Uint16(raw[chunkStart+14 : chunkStart+16])
			haveFmt = true
		case "data":
			dataStart = chunkStart
			dataEnd = chunkEnd
			haveData = true
		}
		// Chunks are word-aligned: odd sizes pad with one byte.
		if size%2 == 1 {
			chunkEnd++
		}
		offset = chunkEnd
	}

	if !haveFmt || !haveData {
		return nil, 0, 0, errors.New("audio: WAV missing fmt or data chunk")
	}
	if audioFormat != 1 {
		return nil, 0, 0, fmt.Errorf("audio: WAV audio format %d not supported (only PCM=1 is accepted)", audioFormat)
	}
	if bps != 16 {
		return nil, 0, 0, fmt.Errorf("audio: WAV bit depth %d not supported (only 16-bit PCM is accepted)", bps)
	}
	if wavChannels == 0 || wavChannels > 8 {
		return nil, 0, 0, fmt.Errorf("audio: WAV channel count %d out of range", wavChannels)
	}
	if wavRate == 0 {
		return nil, 0, 0, errors.New("audio: WAV sample rate is zero")
	}

	return raw[dataStart:dataEnd], int(wavRate), int(wavChannels), nil
}
