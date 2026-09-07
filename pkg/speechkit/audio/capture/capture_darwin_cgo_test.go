//go:build darwin && cgo

package capture

import (
	"errors"
	"strings"
	"testing"

	"github.com/gen2brain/malgo"
)

func TestDarwinMalgoSeamIsCoreAudioWithoutLoopback(t *testing.T) {
	if got := platformMalgoBackend(); got != BackendDarwinCoreAudioMalgo {
		t.Fatalf("platformMalgoBackend() = %q, want %q", got, BackendDarwinCoreAudioMalgo)
	}
	if got := defaultBackend(); got != BackendDarwinCoreAudioMalgo {
		t.Fatalf("defaultBackend() = %q, want %q", got, BackendDarwinCoreAudioMalgo)
	}
	if platformSupportsLoopback() {
		t.Fatal("platformSupportsLoopback() = true on darwin, want false until kombify-SpeechKit-mcos.16")
	}
	backends := platformMalgoContextBackends()
	if len(backends) != 1 || backends[0] != malgo.BackendCoreaudio {
		t.Fatalf("platformMalgoContextBackends() = %v, want [BackendCoreaudio]", backends)
	}
}

// System-audio sources are refused at construction, before any CoreAudio
// context or device exists, so this passes on a headless runner.
func TestOpenRefusesSystemAudioSourcesOnDarwin(t *testing.T) {
	for _, source := range []InputSource{InputSourceSystemLoopback, InputSourceMicAndSystem} {
		t.Run(string(source), func(t *testing.T) {
			session, err := Open(Config{InputSource: source})
			if session != nil {
				_ = session.Close()
				t.Fatalf("Open(%q) returned a session on darwin", source)
			}
			if !errors.Is(err, ErrUnsupportedSource) {
				t.Fatalf("Open(%q) error = %v, want ErrUnsupportedSource", source, err)
			}
			if !strings.Contains(err.Error(), "darwin") {
				t.Fatalf("Open(%q) error = %q, want it to name the platform", source, err)
			}
		})
	}
}

func TestEnsureLoopbackOutputDeviceAvailableFailsClosedOnDarwin(t *testing.T) {
	err := ensureLoopbackOutputDeviceAvailable(Config{InputSource: InputSourceSystemLoopback})
	if !errors.Is(err, ErrUnsupportedSource) {
		t.Fatalf("ensureLoopbackOutputDeviceAvailable() error = %v, want ErrUnsupportedSource", err)
	}
}

// TestListCaptureDevicesOnDarwinIsErrorFree is the headless-runner contract
// from kombify-SpeechKit-mcos.5: a macos-14 runner has no audio input device
// and grants no microphone permission, and enumeration must still answer with
// an empty list instead of an error. The desktop calls this on every settings
// open, so an error here would surface as a broken device picker.
func TestListCaptureDevicesOnDarwinIsErrorFree(t *testing.T) {
	devices, err := ListCaptureDevices(Config{})
	if err != nil {
		t.Fatalf("ListCaptureDevices: %v", err)
	}
	for _, device := range devices {
		if strings.TrimSpace(device.Name) == "" {
			t.Fatalf("device %q has an empty name", device.ID)
		}
	}
	t.Logf("CoreAudio reported %d capture device(s)", len(devices))
}
