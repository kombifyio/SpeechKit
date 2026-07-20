//go:build windows && cgo

package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"
	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword/sherpa"
)

func main() {
	modelDir := flag.String("model-dir", "", "sherpa KWS model directory")
	keywords := flag.String("keywords", "", "BPE-tokenized keywords file")
	wavPath := flag.String("wav", "", "16 kHz mono PCM16 WAV to test")
	phrase := flag.String("phrase", "", "display phrase for detections")
	threshold := flag.Float64("threshold", 0.25, "keyword threshold")
	flag.Parse()

	if *modelDir == "" || *keywords == "" || *wavPath == "" {
		fmt.Fprintln(os.Stderr, "usage: kws-smoke --model-dir <dir> --keywords <keywords.txt> --wav <audio.wav> [--phrase \"hey jarvis\"]")
		os.Exit(2)
	}

	pcm, sampleRate, channels, err := readPCM16WAV(*wavPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if sampleRate != wakeword.SampleRate || channels != wakeword.Channels {
		fmt.Fprintf(os.Stderr, "unsupported WAV format: got %d Hz/%d channels, want %d Hz/%d channel\n", sampleRate, channels, wakeword.SampleRate, wakeword.Channels)
		os.Exit(1)
	}

	detector, err := sherpa.NewDetector(sherpa.DetectorConfig{
		Encoder:      filepath.Join(*modelDir, "encoder-epoch-12-avg-2-chunk-16-left-64.onnx"),
		Decoder:      filepath.Join(*modelDir, "decoder-epoch-12-avg-2-chunk-16-left-64.onnx"),
		Joiner:       filepath.Join(*modelDir, "joiner-epoch-12-avg-2-chunk-16-left-64.onnx"),
		Tokens:       filepath.Join(*modelDir, "tokens.txt"),
		KeywordsFile: *keywords,
		Threshold:    float32(*threshold),
		NumThreads:   1,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer detector.Close()

	detections := 0
	pipeline, err := wakeword.NewPipeline(detector, wakeword.SinkFunc(func(ev wakeword.DetectionEvent) {
		detections++
		fmt.Printf("DETECTED phrase=%q keyword=%q mode=%q probability=%.2f\n", ev.Phrase, ev.Keyword, ev.Mode, ev.Probability)
	}), wakeword.Config{
		Phrase:               *phrase,
		DefaultMode:          "assist",
		Threshold:            float32(*threshold),
		MinConsecutiveFrames: 1,
		Cooldown:             0,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer pipeline.Close()

	totalDecodes := 0
	for offset := 0; offset < len(pcm); offset += wakeword.FrameBytes {
		end := offset + wakeword.FrameBytes
		if end > len(pcm) {
			end = len(pcm)
		}
		decodes, _, err := pipeline.FeedPCM(pcm[offset:end])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		totalDecodes += decodes
	}
	silence := make([]byte, wakeword.FrameBytes*10)
	for i := 0; i < 10; i++ {
		decodes, _, err := pipeline.FeedPCM(silence)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		totalDecodes += decodes
	}
	fmt.Printf("SUMMARY detections=%d decodes=%d wav=%s\n", detections, totalDecodes, *wavPath)
	if detections == 0 {
		os.Exit(3)
	}
}

func readPCM16WAV(path string) ([]byte, int, int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("read wav: %w", err)
	}
	if len(raw) < 44 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WAVE" {
		return nil, 0, 0, fmt.Errorf("unsupported WAV: missing RIFF/WAVE header")
	}

	var (
		audioFormat   uint16
		channels      uint16
		sampleRate    uint32
		bitsPerSample uint16
		data          []byte
	)
	for pos := 12; pos+8 <= len(raw); {
		id := string(raw[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(raw[pos+4 : pos+8]))
		pos += 8
		if size < 0 || pos+size > len(raw) {
			return nil, 0, 0, fmt.Errorf("unsupported WAV: invalid chunk %q", id)
		}
		chunk := raw[pos : pos+size]
		switch id {
		case "fmt ":
			if len(chunk) < 16 {
				return nil, 0, 0, fmt.Errorf("unsupported WAV: short fmt chunk")
			}
			audioFormat = binary.LittleEndian.Uint16(chunk[0:2])
			channels = binary.LittleEndian.Uint16(chunk[2:4])
			sampleRate = binary.LittleEndian.Uint32(chunk[4:8])
			bitsPerSample = binary.LittleEndian.Uint16(chunk[14:16])
		case "data":
			data = chunk
		}
		pos += size
		if size%2 == 1 {
			pos++
		}
	}
	if audioFormat != 1 {
		return nil, 0, 0, fmt.Errorf("unsupported WAV format %d", audioFormat)
	}
	if bitsPerSample != 16 {
		return nil, 0, 0, fmt.Errorf("unsupported WAV bit depth %d", bitsPerSample)
	}
	if len(data) == 0 {
		return nil, 0, 0, fmt.Errorf("unsupported WAV: missing data chunk")
	}
	return data, int(sampleRate), int(channels), nil
}
