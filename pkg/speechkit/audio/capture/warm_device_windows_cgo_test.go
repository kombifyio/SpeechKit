//go:build windows && cgo

package capture

import (
	"testing"

	"github.com/gen2brain/malgo"
)

func TestFrameSinkFollowsDispatchLifecycle(t *testing.T) {
	s := &MalgoSession{cfg: Config{SampleRate: 16000, Channels: 1}, events: make(chan Event, 8)}
	if s.frameSink.Load() != nil {
		t.Fatal("a fresh session must have no frame sink")
	}

	frames := s.startFrameDispatch()
	sink := s.frameSink.Load()
	if sink == nil || *sink != frames {
		t.Fatal("startFrameDispatch must publish its channel as the frame sink")
	}

	s.stopFrameDispatch()
	if s.frameSink.Load() != nil {
		t.Fatal("stopFrameDispatch must detach the frame sink before closing the channel")
	}

	// A second recording on the same (warm) device gets a fresh sink.
	second := s.startFrameDispatch()
	if got := s.frameSink.Load(); got == nil || *got != second || second == frames {
		t.Fatal("the next dispatch must publish a new channel")
	}
	s.stopFrameDispatch()
	close(s.events)
}

func TestDeviceKeyDistinguishesEndpointsAndDefault(t *testing.T) {
	var a, b malgo.DeviceID
	a[0], b[0] = 1, 2
	if deviceKey(malgo.Capture, a, true) == deviceKey(malgo.Capture, b, true) {
		t.Fatal("different endpoints must have different keys")
	}
	if deviceKey(malgo.Capture, a, true) == deviceKey(malgo.Capture, a, false) {
		t.Fatal("a specific endpoint and the default device must have different keys")
	}
	if deviceKey(malgo.Capture, a, false) != deviceKey(malgo.Capture, b, false) {
		t.Fatal("the default device key must not depend on a stale id")
	}
	if deviceKey(malgo.Capture, a, true) == deviceKey(malgo.Loopback, a, true) {
		t.Fatal("capture and loopback of the same endpoint must have different keys")
	}
}

func TestReleaseDeviceWithoutDeviceIsSafe(t *testing.T) {
	s := &MalgoSession{}
	s.releaseDevice()
	s.releaseDevice()
}
