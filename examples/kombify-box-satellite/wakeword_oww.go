//go:build windows && cgo

// openWakeWord-Backend fuer den Box-Companion: spawnt den
// speechkit-openwakeword-Sidecar (eigener Prozess wegen des
// onnxruntime/sherpa-ABI-Konflikts), liest dessen JSON-Events von stdout und
// speist Detections in denselben Kanal wie das sherpa-Backend. Das
// hey_kombify.onnx-Modell erkennt das Marken-Wakeword, an dem das englische
// sherpa-KWS-Modell scheitert (Kunstwort + deutsche Aussprache).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"
)

type owwProcess struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
}

// owwEvent ist die Teilmenge des Sidecar-IPC-Schemas, die der Companion
// konsumiert (cmd/speechkit-openwakeword/ipc.go bleibt der Vertrag).
type owwEvent struct {
	Type        string  `json:"type"`
	Keyword     string  `json:"keyword,omitempty"`
	Phrase      string  `json:"phrase,omitempty"`
	Probability float32 `json:"probability,omitempty"`
	Mode        string  `json:"mode,omitempty"`
	Level       string  `json:"level,omitempty"`
	Msg         string  `json:"msg,omitempty"`
	DeviceName  string  `json:"deviceName,omitempty"`
	DeviceKind  string  `json:"deviceKind,omitempty"`
	Score       float32 `json:"score,omitempty"`
	BytesIn     int64   `json:"bytesIn,omitempty"`
}

// startOpenWakeWord startet den Sidecar und pumpt Detections in sink.
func startOpenWakeWord(cfg *Config, defaultMode string, sink func(wakeword.DetectionEvent)) (*owwProcess, error) {
	exeDir := "."
	if self, err := os.Executable(); err == nil {
		exeDir = filepath.Dir(self)
	}
	sidecar := filepath.Join(exeDir, "speechkit-openwakeword.exe")
	if _, err := os.Stat(sidecar); err != nil {
		return nil, fmt.Errorf("openwakeword sidecar fehlt neben der Companion-exe: %s (go build ./cmd/speechkit-openwakeword)", sidecar)
	}

	modelDir := cfg.owwModelDir()
	wakeModel := filepath.Join(modelDir, cfg.owwPhraseModelFile())
	for _, f := range []string{
		filepath.Join(modelDir, "melspectrogram.onnx"),
		filepath.Join(modelDir, "embedding_model.onnx"),
		wakeModel,
	} {
		if _, err := os.Stat(f); err != nil {
			return nil, fmt.Errorf("openwakeword model fehlt: %s (scripts/prepare-wakeword-model.ps1 laedt die Modelle)", f)
		}
	}

	args := []string{
		"-melspec-model", filepath.Join(modelDir, "melspectrogram.onnx"),
		"-embedding-model", filepath.Join(modelDir, "embedding_model.onnx"),
		"-wake-model", wakeModel,
		"-onnxruntime", cfg.owwOnnxRuntime(),
		"-phrase", cfg.Wakeword.Phrase,
		"-phrase-id", strings.TrimSuffix(cfg.owwPhraseModelFile(), ".onnx"),
		"-default-mode", defaultMode,
		"-threshold", fmt.Sprintf("%.3f", cfg.owwThreshold()),
		"-min-consecutive-frames", fmt.Sprintf("%d", cfg.Wakeword.MinFrames),
		"-cooldown-ms", fmt.Sprintf("%d", int(cfg.cooldown()/time.Millisecond)),
		"-audio-device-hint", cfg.Box.InputDeviceHint,
		// Score-Events sind die einzige Moeglichkeit, Beinahe-Treffer zu
		// sehen; der Companion aggregiert sie unten zu einer 2s-Peak-Zeile.
		"-debug",
	}

	cmd := exec.Command(sidecar, args...)
	cmd.Dir = exeDir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("openwakeword sidecar start: %w", err)
	}

	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 4096), 1<<20)
		var scorePeak float32
		var scoreCount int
		lastScoreLog := time.Now()
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			var ev owwEvent
			if err := json.Unmarshal([]byte(line), &ev); err != nil {
				continue
			}
			if ev.Type == "score" {
				scoreCount++
				if ev.Score > scorePeak {
					scorePeak = ev.Score
				}
				if time.Since(lastScoreLog) >= 2*time.Second {
					log.Printf("[wakeword] score peak=%.3f decodes=%d (threshold=%.2f)", scorePeak, scoreCount, cfg.owwThreshold())
					scorePeak, scoreCount = 0, 0
					lastScoreLog = time.Now()
				}
				continue
			}
			switch ev.Type {
			case "detection":
				sink(wakeword.DetectionEvent{
					Phrase:      ev.Phrase,
					Keyword:     ev.Keyword,
					Mode:        ev.Mode,
					Probability: ev.Probability,
					At:          time.Now(),
				})
			case "ready":
				log.Printf("[wakeword] openwakeword bereit: %q threshold=%.2f", ev.Phrase, cfg.owwThreshold())
			case "device":
				log.Printf("[wakeword] capture device: %s (%s)", ev.DeviceName, ev.DeviceKind)
			case "error":
				log.Printf("[wakeword] sidecar error: %s", ev.Msg)
			case "log":
				if ev.Level == "warn" || ev.Level == "error" {
					log.Printf("[wakeword] sidecar %s: %s", ev.Level, ev.Msg)
				}
			}
		}
	}()

	return &owwProcess{cmd: cmd, stdin: stdin}, nil
}

// Close bittet den Sidecar um Shutdown und raeumt den Prozess ab.
func (p *owwProcess) Close() {
	if p == nil || p.cmd == nil {
		return
	}
	_, _ = io.WriteString(p.stdin, `{"type":"shutdown"}`+"\n")
	done := make(chan struct{})
	go func() { _ = p.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = p.cmd.Process.Kill()
		<-done
	}
}
