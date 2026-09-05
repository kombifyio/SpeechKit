package meeting

import (
	"testing"
	"time"
)

func TestDetectCallIgnoresSpeechKitsOwnMicrophoneUse(t *testing.T) {
	// SpeechKit and its wake-word helper hold the microphone whenever the app
	// runs. Treating that as a call would offer to take notes in the meeting it
	// is already recording.
	users := []MicrophoneUser{
		{App: "SpeechKit.exe", Since: time.Now()},
		{App: "speechkit-wakeword.exe", Since: time.Now()},
	}

	if _, found := DetectCall(users, CallApps(nil)); found {
		t.Fatal("SpeechKit detected itself as a meeting")
	}
}

func TestDetectCallFindsACallInABrowser(t *testing.T) {
	// A browser recording is a call in a tab. Watching a video never touches
	// the microphone, so playback cannot be confused for one.
	users := []MicrophoneUser{{App: "chrome.exe", Since: time.Now()}}

	detection, found := DetectCall(users, CallApps(nil))

	if !found {
		t.Fatal("a browser holding the microphone was not treated as a call")
	}
	if detection.App != "chrome.exe" {
		t.Fatalf("detected %q", detection.App)
	}
}

func TestDetectCallPrefersWhicheverStartedFirst(t *testing.T) {
	now := time.Now()
	users := []MicrophoneUser{
		{App: "slack.exe", Since: now},
		{App: "ms-teams.exe", Since: now.Add(-20 * time.Minute)},
	}

	detection, found := DetectCall(users, CallApps(nil))

	if !found || detection.App != "ms-teams.exe" {
		t.Fatalf("expected the call already in progress, got %+v (found=%v)", detection, found)
	}
}

func TestDetectCallMatchesPackagedTeams(t *testing.T) {
	// Store applications appear under a package family name with a publisher
	// suffix, which is how the new Teams shows up.
	users := []MicrophoneUser{{App: "MSTeams_8wekyb3d8bbwe", Since: time.Now()}}

	if _, found := DetectCall(users, CallApps(nil)); !found {
		t.Fatal("packaged Teams was not recognised")
	}
}

func TestDetectCallIgnoresApplicationsNobodyCallsFrom(t *testing.T) {
	users := []MicrophoneUser{{App: "obs64.exe", Since: time.Now()}}

	if _, found := DetectCall(users, CallApps(nil)); found {
		t.Fatal("a recording tool was announced as a meeting")
	}
}

func TestCallAppsHonoursAConfiguredList(t *testing.T) {
	allowlist := CallApps([]string{" MyDialer.exe ", ""})

	if len(allowlist) != 1 || allowlist[0] != "mydialer.exe" {
		t.Fatalf("configured allowlist = %v", allowlist)
	}
	if _, found := DetectCall([]MicrophoneUser{{App: "chrome.exe"}}, allowlist); found {
		t.Fatal("a configured allowlist should replace the defaults, not extend them")
	}
}
