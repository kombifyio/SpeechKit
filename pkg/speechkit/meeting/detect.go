package meeting

import (
	"strings"
	"time"
)

// Meetings are detected rather than scheduled: the user should not have to tell
// SpeechKit that a call started when the machine already knows.
//
// The signal is that a calling application is using the microphone. Presence of
// the application says nothing — Teams and Slack run all day — while holding the
// microphone is what actually distinguishes a call from an idle app, and it
// covers browser-based calls too without a per-vendor integration. It says
// nothing about who is on the call or what is said; it is a process name and a
// yes-or-no.

// MicrophoneUser is one application recording right now, as reported by the
// host. It mirrors micuse.Session without binding this package to Windows.
type MicrophoneUser struct {
	App   string
	Since time.Time
}

// Detection is a call the user could take notes in.
type Detection struct {
	App   string
	Since time.Time
}

// defaultCallApps are the applications whose microphone use means a call.
// Browsers are included deliberately: a browser recording is a call in a tab,
// and leaving them out would miss Google Meet entirely. Media playback does not
// touch the microphone, so watching a video never looks like a meeting.
var defaultCallApps = []string{
	"ms-teams.exe",
	"teams.exe",
	"msteams",
	"microsoftteams",
	"zoom.exe",
	"cpthost.exe",
	"slack.exe",
	"discord.exe",
	"webexmta.exe",
	"webex.exe",
	"chrome.exe",
	"msedge.exe",
	"firefox.exe",
	"brave.exe",
	"opera.exe",
	"vivaldi.exe",
	"arc.exe",
}

// ownProcesses are SpeechKit's own microphone users. Without excluding them the
// detector would announce a meeting every time the app or its wake-word helper
// opened the microphone — including the meeting it is already recording.
var ownProcesses = []string{
	"speechkit.exe",
	"speechkit-wakeword.exe",
	"speechkit.test.exe",
	"whisper-server.exe",
}

// CallApps returns the detection allowlist, falling back to the built-in set.
func CallApps(configured []string) []string {
	cleaned := make([]string, 0, len(configured))
	for _, app := range configured {
		if app = strings.ToLower(strings.TrimSpace(app)); app != "" {
			cleaned = append(cleaned, app)
		}
	}
	if len(cleaned) == 0 {
		return append([]string(nil), defaultCallApps...)
	}
	return cleaned
}

// DetectCall picks the call out of everything currently using the microphone.
//
// The earliest-started match wins, so a browser that joined a call before a
// chat app opened its microphone is the one reported.
func DetectCall(users []MicrophoneUser, allowlist []string) (Detection, bool) {
	var best Detection
	found := false
	for _, user := range users {
		app := strings.ToLower(strings.TrimSpace(user.App))
		if app == "" || isOwnProcess(app) || !matchesCallApp(app, allowlist) {
			continue
		}
		if !found || user.Since.Before(best.Since) {
			best = Detection{App: user.App, Since: user.Since}
			found = true
		}
	}
	return best, found
}

func isOwnProcess(app string) bool {
	for _, own := range ownProcesses {
		if app == own {
			return true
		}
	}
	return false
}

// matchesCallApp compares on the whole name, except for the packaged Store
// entries whose keys carry a publisher suffix ("MSTeams_8wekyb3d8bbwe").
func matchesCallApp(app string, allowlist []string) bool {
	for _, candidate := range allowlist {
		if app == candidate {
			return true
		}
		if !strings.HasSuffix(candidate, ".exe") && strings.HasPrefix(app, candidate+"_") {
			return true
		}
	}
	return false
}
