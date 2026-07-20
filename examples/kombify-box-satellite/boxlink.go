//go:build windows && cgo

// boxlink schreibt Companion-Statuszeilen ("KBX <state>") direkt auf den
// CDC-ACM-Port der kombify box und ersetzt damit die fruehere
// run-companion.ps1-Regex-Bridge. Die Firmware (firmware/main/box_status.c im
// kombify-box-Repo) parst zeilenweise Text: idle|wake|listening|thinking|
// speaking|done|error, optional mit "KBX "-Praefix.
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

// Der USB-Composite der Box (UAC + CDC) meldet sich mit Espressifs VID und
// der in firmware/sdkconfig.defaults gesetzten PID.
const (
	boxUSBVID = "303A"
	boxUSBPID = "8000"
)

// BoxLink haelt den seriellen Statuskanal zur Box. Ein nil-*BoxLink ist
// gueltig und macht alle Methoden zu No-ops, damit der Companion ohne
// angeschlossene Box (oder mit status_port = "off") unveraendert laeuft.
type BoxLink struct {
	mu     sync.Mutex
	port   serial.Port
	name   string
	hint   string // expliziter Port-Wunsch fuer Reconnects ("" = Autodetect)
	closed bool
}

// OpenBoxLink oeffnet den CDC-Statusport. portHint waehlt einen expliziten
// COM-Port ("COM7"); leer = KOMBIFY_BOX_STATUS_PORT-Env oder Autodetect ueber
// USB VID/PID; "off" = deaktiviert (nil, nil).
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

// startReader liest den CDC-Log-Mirror der Box (durch DTR aktiv) und spiegelt
// relevante Zeilen ins Companion-Log: WLAN-Status, Touch-Events, Fehler.
// Die 1-Hz-Mic-Diagnose und sonstiges Boot-Rauschen werden verworfen.
// KBX_BOX_LOG_ALL=1 loggt ungefiltert. Endet still beim ersten Read-Fehler;
// der Reconnect in SendLine startet einen neuen Reader.
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
	// ESP-IDF-Fehler-/Warn-Level (E/W + Klammer-Timestamp), z. B. "E (1234) ..."
	return strings.HasPrefix(s, "E (") || strings.HasPrefix(s, "W (")
}

func openBoxPort(name string) (serial.Port, error) {
	port, err := serial.Open(name, &serial.Mode{BaudRate: 115200})
	if err != nil {
		return nil, err
	}
	// DTR signalisiert der Firmware einen verbundenen Host
	// (tud_cdc_connected) und schaltet den CDC-Log-Mirror frei.
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
	return "", fmt.Errorf("boxlink: kein CDC-Port mit VID %s / PID %s gefunden (Box angeschlossen? status_port setzen?)", boxUSBVID, boxUSBPID)
}

// SetStage uebersetzt eine companion.Stage in die v1-Statuszeile der Firmware.
func (b *BoxLink) SetStage(stage companion.Stage) {
	state := stageToBoxState(stage)
	if state == "" {
		return
	}
	if err := b.SendLine("KBX " + state); err != nil {
		log.Printf("[boxlink] %v", err)
	}
}

// SendLine schreibt eine newline-terminierte Rohzeile (v2-Escape-Hatch, z. B.
// kuenftig "KBX vu 42").
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
		// Gestallter oder re-enumerierter CDC-Port (z. B. Box-Reboot,
		// USB-Suspend): Handle wegwerfen und einmal neu verbinden.
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

// reopenLocked verbindet den Statusport neu (Autodetect, falls kein
// expliziter Port konfiguriert ist). Caller haelt b.mu.
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
	log.Printf("[boxlink] Status-UI neu verbunden: %s", name)
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

// stageToBoxState mappt die Companion-Stages 1:1 auf das Vokabular von
// box_status.c. Unbekannte Stages werden verworfen statt geraten.
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
