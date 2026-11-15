package edgesetup

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// WakeStatus is the device's local wake-word state, read over the console.
//
// The Box reports the closed set of phrases its firmware actually runs
// ("config: reported ... phrases=<id>,<id>"), so a setup flow offers only
// what the attached device can recognize. A phrase id the device does not
// run is never offered and never written.
type WakeStatus struct {
	// Enabled is the device's wake-word switch.
	Enabled bool `json:"enabled"`
	// Ready is true when the engine runs without an error.
	Ready bool `json:"ready"`
	// Error is the engine's error name ("ESP_OK" when healthy).
	Error string `json:"error,omitempty"`
	// Phrase is the selected phrase id, empty when the device reports none.
	Phrase string `json:"phrase,omitempty"`
	// Phrases is every phrase id this firmware can recognize.
	Phrases []string `json:"phrases"`
}

// configReported matches the device's reported line:
//
//	config: reported firmware=<rev> wake enabled=1 ready=1 error=ESP_OK phrases=<id>,<id>
var configReported = regexp.MustCompile(`wake enabled=(\S+) ready=(\S+) error=(\S+) phrases=(\S*)`)

// phraseLine matches one line of the device's phrase list:
//
//	wake phrase <id> [selected]: "<label>" (mww <id>, default thr 0.500)
var phraseLine = regexp.MustCompile(`wake phrase ([a-z0-9_]+)( \[selected\])?:`)

// ReadWake asks the device for its wake-word state.
func ReadWake(ctx context.Context, l *Link) (WakeStatus, error) {
	line, err := l.Ask(ctx, "KBX config?", "config: reported", 5*time.Second)
	if err != nil {
		return WakeStatus{}, err
	}
	m := configReported.FindStringSubmatch(line)
	if m == nil {
		return WakeStatus{}, errors.New("edgesetup: the device did not report its wake-word state")
	}
	out := WakeStatus{
		Enabled: m[1] == "1",
		Ready:   m[2] == "1",
		Error:   m[3],
		Phrases: splitPhrases(m[4]),
	}
	// The selected phrase is a separate line of the phrase list; reading it
	// costs one more round trip, so a failure leaves it empty rather than
	// failing the whole read.
	if p, err := readSelectedPhrase(ctx, l, len(out.Phrases)); err == nil {
		out.Phrase = p
	}
	return out, nil
}

// readSelectedPhrase reads the selected phrase id from the phrase list. The
// device logs one line per phrase and marks the current one "[selected]".
//
// The list has no end marker and the reported summary line can be truncated
// by the firmware, so the count of reported phrases is only a lower bound.
// The read therefore stops when the selected marker was seen, or after every
// reported phrase plus one line, so it never waits for the full timeout.
func readSelectedPhrase(ctx context.Context, l *Link, count int) (string, error) {
	if count <= 0 {
		return "", nil
	}
	l.Drain()
	if err := l.Send("KBX wake phrase ?"); err != nil {
		return "", err
	}
	selected := ""
	seen := 0
	limit := count + 1
	err := l.Expect(ctx, 5*time.Second, func(line string) (bool, error) {
		m := phraseLine.FindStringSubmatch(line)
		if m == nil {
			return false, nil
		}
		seen++
		if m[2] != "" {
			selected = m[1]
			return true, nil
		}
		return seen >= limit, nil
	})
	if err != nil {
		return "", err
	}
	return selected, nil
}

func splitPhrases(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// SetWakePhrase selects a local wake word on the device
// (`KBX wake phrase <id>`) and waits until the device reports it.
//
// The phrase must be one the device itself lists: an id the device does not
// run is refused here, before it reaches the device, because a wake word
// that cannot be recognized is worse than the default.
func SetWakePhrase(ctx context.Context, l *Link, phrase string) (WakeStatus, error) {
	if !validPhraseID(phrase) {
		return WakeStatus{}, errors.New("edgesetup: that wake word is not usable")
	}
	current, err := ReadWake(ctx, l)
	if err != nil {
		return WakeStatus{}, err
	}
	if !contains(current.Phrases, phrase) {
		return current, fmt.Errorf("edgesetup: this device cannot recognize %q", phrase)
	}
	if current.Phrase == phrase {
		return current, nil
	}
	l.Drain()
	if err := l.Send("KBX wake phrase " + phrase); err != nil {
		return current, err
	}
	// The device logs the switch; wait for it so the caller can report what
	// the device actually selected. The switch line is the quoted form
	// ("wake: phrase \"Hey Kubi\" (...)"); the periodic status line reads
	// "wake: phrase=<id>" and is not a switch.
	if err := l.Expect(ctx, 5*time.Second, func(line string) (bool, error) {
		return strings.Contains(line, `wake: phrase "`), nil
	}); err != nil {
		return current, err
	}
	after, err := ReadWake(ctx, l)
	if err != nil {
		return current, nil
	}
	return after, nil
}

// validPhraseID accepts only the ids the firmware uses: lower-case letters,
// digits and underscores. A phrase id travels into a console line, so it may
// not carry spaces, quotes or line breaks.
func validPhraseID(id string) bool {
	if id == "" || len(id) > 15 {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
