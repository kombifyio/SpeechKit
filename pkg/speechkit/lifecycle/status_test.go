package lifecycle

import "testing"

func TestStatusIsActive(t *testing.T) {
	cases := []struct {
		s    Status
		want bool
	}{
		{StatusStopped, false},
		{StatusStarting, true},
		{StatusRunning, true},
		{StatusStopping, false},
		{StatusFailed, false},
		{StatusDisabled, false},
	}
	for _, tc := range cases {
		if got := tc.s.IsActive(); got != tc.want {
			t.Errorf("Status(%q).IsActive() = %v; want %v", tc.s, got, tc.want)
		}
	}
}

func TestStatusIsTerminal(t *testing.T) {
	cases := []struct {
		s    Status
		want bool
	}{
		{StatusStopped, true},
		{StatusStarting, false},
		{StatusRunning, false},
		{StatusStopping, false},
		{StatusFailed, true},
		{StatusDisabled, true},
	}
	for _, tc := range cases {
		if got := tc.s.IsTerminal(); got != tc.want {
			t.Errorf("Status(%q).IsTerminal() = %v; want %v", tc.s, got, tc.want)
		}
	}
}

func TestStatusRankOrdering(t *testing.T) {
	// Failed must outrank everything; Running outranks the in-flight
	// states; Stopped/Disabled tie at the bottom.
	if StatusFailed.Rank() <= StatusRunning.Rank() {
		t.Errorf("Failed should rank above Running")
	}
	if StatusRunning.Rank() <= StatusStarting.Rank() {
		t.Errorf("Running should rank above Starting")
	}
	if StatusStarting.Rank() != StatusStopping.Rank() {
		t.Errorf("Starting and Stopping should tie at the in-flight rank")
	}
	if StatusStopped.Rank() != StatusDisabled.Rank() {
		t.Errorf("Stopped and Disabled should tie at the rest rank")
	}
	if StatusStopped.Rank() >= StatusStarting.Rank() {
		t.Errorf("Stopped should rank below Starting")
	}
}

func TestStatusStringFallback(t *testing.T) {
	if got := Status("").String(); got != string(StatusStopped) {
		t.Errorf("empty Status.String() = %q; want %q (fallback to stopped)", got, StatusStopped)
	}
	if got := StatusRunning.String(); got != "running" {
		t.Errorf("StatusRunning.String() = %q; want %q", got, "running")
	}
}
