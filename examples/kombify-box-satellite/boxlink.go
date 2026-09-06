//go:build windows && cgo

// boxlink writes companion status lines ("KBX <state>") directly to the
// CDC-ACM port of the kombify box and thereby replaces the earlier
// run-companion.ps1 regex bridge. The firmware (firmware/main/box_status.c in
// the kombify-box repo) parses line-wise text: idle|wake|listening|thinking|
// speaking|done|error, optionally with a "KBX " prefix.
package main

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/companion"
)

// The Box's USB composite device (UAC + CDC) reports Espressif's VID and the
// PID set in firmware/sdkconfig.defaults.
const (
	boxUSBVID = "303A"
	boxUSBPID = "8000"
)

// BoxLink holds the serial status channel to the Box. A nil *BoxLink is
// valid and turns every method into a no-op, so the companion runs unchanged
// without a connected Box (or with status_port = "off").
type BoxLink struct {
	mu     sync.Mutex
	port   serial.Port
	name   string
	hint   string // explicit port wish for reconnects ("" = autodetect)
	closed bool
}

// OpenBoxLink opens the CDC status port. portHint selects an explicit COM
// port ("COM7"); empty = the KOMBIFY_BOX_STATUS_PORT env or autodetect via
// USB VID/PID; "off" = disabled (nil, nil).
func OpenBoxLink(portHint string) (*BoxLink, error) {
	portHint = strings.TrimSpace(portHint)
	if portHint == "" {
		portHint = strings.TrimSpace(os.Getenv("KOMBIFY_BOX_STATUS_PORT"))
	}
	if strings.EqualFold(portHint, "off") {
		return nil, nil
	}
	name := portHint
	if name == "" {
		detected, err := detectBoxPort()
		if err != nil {
			return nil, err
		}
		name = detected
	}
	port, err := openBoxPort(name)
	if err != nil {
		return nil, fmt.Errorf("boxlink: open %s: %w", name, err)
	}
	b := &BoxLink{port: port, name: name, hint: portHint}
	b.startReader(port)
	return b, nil
}

// startReader reads the Box's CDC log mirror (active through DTR) and mirrors
// relevant lines into the companion log: Wi-Fi status, touch events, errors.
// The 1 Hz mic diagnostics and other boot noise are discarded.
// KBX_BOX_LOG_ALL=1 logs unfiltered. Ends silently on the first read error;
// the reconnect in SendLine starts a new reader.
func (b *BoxLink) startReader(port serial.Port) {
	logAll := os.Getenv("KBX_BOX_LOG_ALL") == "1"
	go func() {
		buf := make([]byte, 512)
		var line []byte
		for {
			n, err := port.Read(buf)
			if err != nil {
				return
			}
			for _, c := range buf[:n] {
				if c == '\r' {
					continue
				}
				if c != '\n' {
					if len(line) < 512 {
						line = append(line, c)
					}
					continue
				}
				s := strings.TrimSpace(string(line))
				line = line[:0]
				if s == "" {
					continue
				}
				if logAll || boxLogRelevant(s) {
					log.Printf("[box] %s", s)
				}
			}
		}
	}()
}

func boxLogRelevant(s string) bool {
	for _, marker := range []string{"net_wifi", "kbx_touch", "oobe", "net_ws", "net_agent"} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	// ESP-IDF error/warning level (E/W + bracketed timestamp), e.g. "E (1234) ..."
	return strings.HasPrefix(s, "E (") || strings.HasPrefix(s, "W (")
}

func openBoxPort(name string) (serial.Port, error) {
	port, err := serial.Open(name, &serial.Mode{BaudRate: 115200})
	if err != nil {
		return nil, err
	}
	// DTR signals a connected host to the firmware (tud_cdc_connected) and
	// unlocks the CDC log mirror.
	_ = port.SetDTR(true)
	return port, nil
}

func detectBoxPort() (string, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return "", fmt.Errorf("boxlink: enumerate ports: %w", err)
	}
	for _, p := range ports {
		if p.IsUSB && strings.EqualFold(p.VID, boxUSBVID) && strings.EqualFold(p.PID, boxUSBPID) {
			return p.Name, nil
		}
	}
	return "", fmt.Errorf("boxlink: no CDC port with VID %s / PID %s found (is the Box connected? set status_port?)", boxUSBVID, boxUSBPID)
}

// SetStage translates a companion.Stage into the firmware's v1 status line.
func (b *BoxLink) SetStage(stage companion.Stage) {
	state := stageToBoxState(stage)
	if state == "" {
		return
	}
	if err := b.SendLine("KBX " + state); err != nil {
		log.Printf("[boxlink] %v", err)
	}
}

// SendLine writes a newline-terminated raw line (v2 escape hatch, e.g. a
// future "KBX vu 42").
func (b *BoxLink) SendLine(line string) error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	if b.port != nil {
		if _, err := b.port.Write([]byte(line + "\n")); err == nil {
			return nil
		}
		// Stalled or re-enumerated CDC port (e.g. Box reboot, USB suspend):
		// drop the handle and reconnect once.
		_ = b.port.Close()
		b.port = nil
	}
	if err := b.reopenLocked(); err != nil {
		return fmt.Errorf("boxlink: reconnect: %w", err)
	}
	if _, err := b.port.Write([]byte(line + "\n")); err != nil {
		return fmt.Errorf("boxlink: write %s: %w", b.name, err)
	}
	return nil
}

// reopenLocked reconnects the status port (autodetect when no explicit port
// is configured). The caller holds b.mu.
func (b *BoxLink) reopenLocked() error {
	name := b.hint
	if name == "" {
		detected, err := detectBoxPort()
		if err != nil {
			return err
		}
		name = detected
	}
	port, err := openBoxPort(name)
	if err != nil {
		return err
	}
	b.port, b.name = port, name
	b.startReader(port)
	log.Printf("[boxlink] status UI reconnected: %s", name)
	return nil
}

func (b *BoxLink) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	if b.port == nil {
		return nil
	}
	err := b.port.Close()
	b.port = nil
	return err
}

// stageToBoxState maps the companion stages 1:1 onto the vocabulary of
// box_status.c. Unknown stages are dropped rather than guessed.
func stageToBoxState(stage companion.Stage) string {
	switch stage {
	case companion.StageWake:
		return "wake"
	case companion.StageListening:
		return "listening"
	case companion.StageThinking:
		return "thinking"
	case companion.StageSpeaking:
		return "speaking"
	case companion.StageIdle:
		return "idle"
	case companion.StageError:
		return "error"
	default:
		return ""
	}
}
