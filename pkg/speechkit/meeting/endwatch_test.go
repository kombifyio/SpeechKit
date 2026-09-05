package meeting

import (
	"testing"
	"time"
)

func newTestEndWatcher(users *[]MicrophoneUser, clock *fakeClock, recording *bool, ended *int) *EndWatcher {
	return &EndWatcher{
		Grace:     45 * time.Second,
		Read:      func() ([]MicrophoneUser, error) { return *users, nil },
		Recording: func() bool { return *recording },
		End:       func() { *ended++ },
		Now:       clock.now,
	}
}

func TestEndWatcherEndsTheMeetingAfterTheCallLeavesTheMicrophone(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	users := []MicrophoneUser{{App: "ms-teams.exe", Since: clock.at}}
	recording := true
	ended := 0
	watcher := newTestEndWatcher(&users, clock, &recording, &ended)

	watcher.Poll()

	// The call ends; the microphone stays free through the grace period.
	users = nil
	for i := 0; i < 12; i++ {
		clock.advance(5 * time.Second)
		watcher.Poll()
	}

	if ended != 1 {
		t.Fatalf("ended %d time(s), want once", ended)
	}
}

func TestEndWatcherEndsOnlyOncePerRecording(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	users := []MicrophoneUser{{App: "ms-teams.exe", Since: clock.at}}
	recording := true
	ended := 0
	watcher := newTestEndWatcher(&users, clock, &recording, &ended)

	watcher.Poll()
	users = nil
	// Stopping drains asynchronously, so the capture reports active for a
	// while after End fired. That must not trigger a second stop.
	for i := 0; i < 30; i++ {
		clock.advance(5 * time.Second)
		watcher.Poll()
	}

	if ended != 1 {
		t.Fatalf("ended %d time(s) while the stop drained, want once", ended)
	}
}

func TestEndWatcherIgnoresAMomentaryMicrophoneGap(t *testing.T) {
	// A headset reconnecting or a device switch drops the microphone for a few
	// seconds mid-call. That is not the meeting ending.
	clock := &fakeClock{at: time.Now()}
	users := []MicrophoneUser{{App: "zoom.exe", Since: clock.at}}
	recording := true
	ended := 0
	watcher := newTestEndWatcher(&users, clock, &recording, &ended)

	watcher.Poll()
	users = nil
	clock.advance(10 * time.Second)
	watcher.Poll()
	users = []MicrophoneUser{{App: "zoom.exe", Since: clock.at}}
	clock.advance(10 * time.Second)
	watcher.Poll()
	users = nil
	clock.advance(30 * time.Second)
	watcher.Poll()

	if ended != 0 {
		t.Fatalf("ended the meeting across a brief device switch (grace not yet over)")
	}
}

func TestEndWatcherNeverEndsAMeetingWithoutACall(t *testing.T) {
	// An in-person meeting recorded by hand never shows a calling application
	// on the microphone. Ending it automatically would throw work away.
	clock := &fakeClock{at: time.Now()}
	users := []MicrophoneUser{}
	recording := true
	ended := 0
	watcher := newTestEndWatcher(&users, clock, &recording, &ended)

	for i := 0; i < 100; i++ {
		watcher.Poll()
		clock.advance(5 * time.Second)
	}

	if ended != 0 {
		t.Fatalf("ended a hand-started recording that never overlapped a call")
	}
}

func TestEndWatcherForgetsTheCallBetweenRecordings(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	users := []MicrophoneUser{{App: "ms-teams.exe", Since: clock.at}}
	recording := true
	ended := 0
	watcher := newTestEndWatcher(&users, clock, &recording, &ended)

	watcher.Poll()
	// The recording stops by hand while the call is still going.
	recording = false
	watcher.Poll()

	// A new hand-started recording begins with no call in sight. The call seen
	// during the previous recording must not count towards ending this one.
	recording = true
	users = nil
	for i := 0; i < 20; i++ {
		clock.advance(5 * time.Second)
		watcher.Poll()
	}

	if ended != 0 {
		t.Fatalf("ended a new recording based on the previous recording's call")
	}
}

func TestEndWatcherSurvivesAFailedReading(t *testing.T) {
	recording := true
	watcher := &EndWatcher{
		Read:      func() ([]MicrophoneUser, error) { return nil, errRead },
		Recording: func() bool { return recording },
		End:       func() { t.Fatal("ended a meeting from a failed reading") },
	}

	watcher.Poll()
}
