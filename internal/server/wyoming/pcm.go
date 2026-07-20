package wyoming

import (
	"encoding/binary"
	"fmt"
)

// pcmToWAV wraps raw S16LE PCM in a 44-byte canonical WAV header so the shared
// audio ingress canonicalizer (internal/server/audio) can downmix/resample it.
// Used for the non-canonical STT path (a satellite that declares a rate other
// than 16 kHz mono S16LE). rate/channels/width are the source parameters;
// width is bytes per sample (2 for S16LE).
func pcmToWAV(pcm []byte, rate, channels, width int) []byte {
	if rate <= 0 {
		rate = 16000
	}
	if channels <= 0 {
		channels = 1
	}
	if width <= 0 {
		width = 2
	}
	byteRate := rate * channels * width
	blockAlign := channels * width
	dataLen := len(pcm)

	buf := make([]byte, 44+dataLen)
	copy(buf[0:4], "RIFF")
	binary.LittleEndian.PutUint32(buf[4:8], uint32(36+dataLen))
	copy(buf[8:12], "WAVE")
	copy(buf[12:16], "fmt ")
	binary.LittleEndian.PutUint32(buf[16:20], 16) // PCM fmt chunk size
	binary.LittleEndian.PutUint16(buf[20:22], 1)  // audio format = PCM
	binary.LittleEndian.PutUint16(buf[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(buf[24:28], uint32(rate))
	binary.LittleEndian.PutUint32(buf[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(buf[32:34], uint16(blockAlign))
	binary.LittleEndian.PutUint16(buf[34:36], uint16(width*8))
	copy(buf[36:40], "data")
	binary.LittleEndian.PutUint32(buf[40:44], uint32(dataLen))
	copy(buf[44:], pcm)
	return buf
}

// parseWAV extracts the PCM data and format from a canonical PCM WAV. It walks
// the RIFF chunks so a non-standard chunk between "fmt " and "data" (e.g. a
// LIST/fact chunk some TTS providers emit) does not break extraction. Only
// uncompressed PCM (audio format 1) is supported.
func parseWAV(b []byte) (pcm []byte, rate, channels, width int, err error) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, 0, 0, 0, fmt.Errorf("wyoming: not a RIFF/WAVE stream")
	}
	var haveFmt, haveData bool
	off := 12
	for off+8 <= len(b) {
		id := string(b[off : off+4])
		size := int(binary.LittleEndian.Uint32(b[off+4 : off+8]))
		body := off + 8
		if size < 0 || body+size > len(b) {
			// Truncated final chunk: clamp so a slightly-short data chunk still
			// yields the audio we have.
			size = len(b) - body
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, 0, 0, 0, fmt.Errorf("wyoming: short fmt chunk")
			}
			format := binary.LittleEndian.Uint16(b[body : body+2])
			if format != 1 {
				return nil, 0, 0, 0, fmt.Errorf("wyoming: unsupported WAV audio format %d (want PCM)", format)
			}
			channels = int(binary.LittleEndian.Uint16(b[body+2 : body+4]))
			rate = int(binary.LittleEndian.Uint32(b[body+4 : body+8]))
			width = int(binary.LittleEndian.Uint16(b[body+14:body+16])) / 8
			haveFmt = true
		case "data":
			pcm = b[body : body+size]
			haveData = true
		}
		// Chunks are word-aligned: an odd size is padded with one byte.
		off = body + size
		if size%2 == 1 {
			off++
		}
		if haveFmt && haveData {
			break
		}
	}
	if !haveFmt || !haveData {
		return nil, 0, 0, 0, fmt.Errorf("wyoming: WAV missing fmt or data chunk")
	}
	if width <= 0 {
		width = 2
	}
	return pcm, rate, channels, width, nil
}
