package meeting

import (
	"testing"
	"time"
)

type fakeClock struct{ at time.Time }

func (c *fakeClock) now() time.Time          { return c.at }
func (c *fakeClock) advance(d time.Duration) { c.at = c.at.Add(d) }

func newTestWatcher(users *[]MicrophoneUser, clock *fakeClock, announced *[]Detection) *Watcher {
	return &Watcher{
		Rearm: time.Minute,
		Read:  func() ([]MicrophoneUser, error) { return *users, nil },
		Announce: func(detection Detection) {
			*announced = append(*announced, detection)
		},
		Now: clock.now,
	}
}

// Being told about the same call on every poll would make the feature
// unusable — the point is one prompt when a call starts.
func TestWatcherAnnouncesACallOnce(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	users := []MicrophoneUser{{App: "ms-teams.exe", Since: clock.at}}
	var announced []Detection
	watcher := newTestWatcher(&users, clock, &announced)

	for i := 0; i < 5; i++ {
		watcher.Poll()
		clock.advance(5 * time.Second)
	}

	if len(announced) != 1 {
		t.Fatalf("announced %d times, want once", len(announced))
	}
}

func TestWatcherAnnouncesTheNextCallAfterTheFirstEnds(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	users := []MicrophoneUser{{App: "ms-teams.exe", Since: clock.at}}
	var announced []Detection
	watcher := newTestWatcher(&users, clock, &announced)

	watcher.Poll()

	// The call ends and the microphone is released.
	users = nil
	clock.advance(90 * time.Second)
	watcher.Poll()

	// A second call starts later in the day.
	users = []MicrophoneUser{{App: "ms-teams.exe", Since: clock.at}}
	watcher.Poll()

	if len(announced) != 2 {
		t.Fatalf("announced %d call(s), want the second one too", len(announced))
	}
}

func TestWatcherDoesNotReannounceAcrossAMomentaryGap(t *testing.T) {
	// Muting, or a device switching mid-call, can drop the microphone briefly.
	// That is the same call, not a new one.
	clock := &fakeClock{at: time.Now()}
	users := []MicrophoneUser{{App: "zoom.exe", Since: clock.at}}
	var announced []Detection
	watcher := newTestWatcher(&users, clock, &announced)

	watcher.Poll()
	users = nil
	clock.advance(10 * time.Second)
	watcher.Poll()
	users = []MicrophoneUser{{App: "zoom.exe", Since: clock.at}}
	watcher.Poll()

	if len(announced) != 1 {
		t.Fatalf("announced %d times across a brief mute, want once", len(announced))
	}
}

func TestWatcherStandsDownWhileAMeetingIsRecorded(t *testing.T) {
	clock := &fakeClock{at: time.Now()}
	users := []MicrophoneUser{{App: "ms-teams.exe", Since: clock.at}}
	var announced []Detection
	watcher := newTestWatcher(&users, clock, &announced)
	recording := true
	watcher.Suspended = func() bool { return recording }

	watcher.Poll()
	if len(announced) != 0 {
		t.Fatal("offered to take notes in the meeting already being recorded")
	}

	// Once that meeting ends, the next call is announced normally.
	recording = false
	watcher.Poll()
	if len(announced) != 1 {
		t.Fatalf("announced %d call(s) after recording stopped, want one", len(announced))
	}
}

func TestWatcherSurvivesAFailedReading(t *testing.T) {
	watcher := &Watcher{
		Read:     func() ([]MicrophoneUser, error) { return nil, errRead },
		Announce: func(Detection) { t.Fatal("announced a call from a failed reading") },
	}

	watcher.Poll()
}

var errRead = errTest("registry unavailable")

type errTest string

func (e errTest) Error() string { return string(e) }
