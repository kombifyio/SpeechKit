//go:build windows && cgo

package main

import (
	"bytes"
	"testing"

	"go.bug.st/serial"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/companion"
)

func TestStageToBoxStateMapsFirmwareVocabulary(t *testing.T) {
	// Die States muessen exakt dem Parser in kombify-box
	// firmware/main/box_status.c entsprechen.
	cases := map[companion.Stage]string{
		companion.StageWake:      "wake",
		companion.StageListening: "listening",
		companion.StageThinking:  "thinking",
		companion.StageSpeaking:  "speaking",
		companion.StageIdle:      "idle",
		companion.StageError:     "error",
		companion.Stage("bogus"): "",
	}
	for stage, want := range cases {
		if got := stageToBoxState(stage); got != want {
			t.Errorf("stageToBoxState(%q) = %q, want %q", stage, got, want)
		}
	}
}

// fakeSerialPort erfuellt serial.Port ueber das eingebettete nil-Interface;
// nur die von BoxLink.SendLine benutzten Methoden sind implementiert. Ein
// Aufruf einer nicht ueberschriebenen Methode panict — das ist gewollt, denn
// dann hat sich der Contract von SendLine geaendert und der Test muss mit.
type fakeSerialPort struct {
	serial.Port
	buf bytes.Buffer
}

func (f *fakeSerialPort) Write(p []byte) (int, error) { return f.buf.Write(p) }
func (f *fakeSerialPort) Close() error                { return nil }

func TestSendLineWireFraming(t *testing.T) {
	// Pinnt das Wire-Format, das firmware/main/usb_log.c zeilenweise parst:
	// "KBX <state>\n" — genau ein Newline, kein CR, Praefix in Grossbuchstaben.
	fake := &fakeSerialPort{}
	b := &BoxLink{port: fake, name: "fake"}
	b.SetStage(companion.StageWake)
	b.SetStage(companion.StageSpeaking)
	if err := b.SendLine("KBX touch?"); err != nil {
		t.Fatalf("SendLine: %v", err)
	}
	want := "KBX wake\nKBX speaking\nKBX touch?\n"
	if got := fake.buf.String(); got != want {
		t.Errorf("wire = %q, want %q", got, want)
	}
}

func TestBoxLogRelevant(t *testing.T) {
	// Pinnt den Box->Host-Filter: welche Firmware-Log-Zeilen der Companion
	// als "[box] ..." spiegelt. Tags muessen zu den Firmware-Modulen passen
	// (net_wifi.c, touch.c "kbx_touch", geplant net_ws/net_agent, oobe).
	cases := map[string]bool{
		"I (1234) net_wifi: status: verbunden mit HomeNet, IP 192.0.2.10":                         true,
		"I (1234) kbx_touch: status: init=ok task=up polls=42 presses=1 actions=1 last=(180,180)": true,
		"I (99) net_ws: session=abc state=listening":                                              true,
		"I (99) net_agent: heartbeat ok":                                                          true,
		"I (7) oobe: setup gestartet":                                                             true,
		"E (1) kbx_audio: i2s kaputt":                                                             true,
		"W (2) tusb: suspend":                                                                     true,
		"I (5000) kbx_audio: mic 1s: frames=100 rms=12":                                           false,
		"I (1) boot: chip revision v0.2":                                                          false,
		"":                                                                                        false,
	}
	for line, want := range cases {
		if got := boxLogRelevant(line); got != want {
			t.Errorf("boxLogRelevant(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestOpenBoxLinkOffDisablesLink(t *testing.T) {
	link, err := OpenBoxLink("off")
	if err != nil {
		t.Fatalf("OpenBoxLink(off): %v", err)
	}
	if link != nil {
		t.Fatal("OpenBoxLink(off) should return a nil link")
	}
	// nil-Empfaenger sind vollstaendig no-op — der Companion darf ohne Box
	// nicht anders laufen als mit.
	link.SetStage(companion.StageSpeaking)
	if err := link.SendLine("KBX tone"); err != nil {
		t.Fatalf("nil BoxLink SendLine: %v", err)
	}
	if err := link.Close(); err != nil {
		t.Fatalf("nil BoxLink Close: %v", err)
	}
}
