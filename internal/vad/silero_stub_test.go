//go:build !cgo

package vad

import (
	"strings"
	"testing"
)

// The stub build exists so non-CGo compilers (e.g. cross-compile targets,
// CI runners without the onnxruntime DLL) can still build packages that
// import vad. These tests verify the stub surface returns the advertised
// "requires cgo" sentinel on every call path, so a consumer that forgot to
// enable CGo gets a clear error at runtime rather than a crash.

func TestStub_NewSileroVAD_ReturnsError(t *testing.T) {
	v, err := NewSileroVAD("ignored")
	if err == nil {
		t.Fatal("NewSileroVAD stub returned nil err, want cgo error")
	}
	if v != nil {
		t.Errorf("NewSileroVAD stub returned non-nil SileroVAD: %v", v)
	}
	if !strings.Contains(err.Error(), "cgo") {
		t.Errorf("stub error = %q, want substring 'cgo'", err.Error())
	}
}

func TestStub_ProcessFrame_ReturnsError(t *testing.T) {
	v := &SileroVAD{}
	prob, err := v.ProcessFrame(make([]int16, FrameSize))
	if err == nil {
		t.Fatal("ProcessFrame stub returned nil err, want cgo error")
	}
	if prob != 0 {
		t.Errorf("ProcessFrame stub prob = %v, want 0", prob)
	}
	if !strings.Contains(err.Error(), "cgo") {
		t.Errorf("stub error = %q, want substring 'cgo'", err.Error())
	}
}

func TestStub_ResetAndClose_AreSafeNoOps(t *testing.T) {
	v := &SileroVAD{}
	// These must not panic on the zero value.
	v.Reset()
	v.Close()
}

func TestStub_DetectorInterfaceSatisfied(t *testing.T) {
	// Guard against accidental signature drift between the stub and the
	// cgo-backed SileroVAD. Consumers treat Detector as the source of truth.
	var _ Detector = (*SileroVAD)(nil)
}

func TestStub_FrameConstants(t *testing.T) {
	if SampleRate != 16000 {
		t.Errorf("SampleRate = %d, want 16000", SampleRate)
	}
	if FrameSize != 512 {
		t.Errorf("FrameSize = %d, want 512", FrameSize)
	}
	if BytesPerSample != 2 {
		t.Errorf("BytesPerSample = %d, want 2", BytesPerSample)
	}
	if FrameBytes != FrameSize*BytesPerSample {
		t.Errorf("FrameBytes = %d, want %d", FrameBytes, FrameSize*BytesPerSample)
	}
}
