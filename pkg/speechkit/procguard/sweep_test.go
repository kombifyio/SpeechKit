package procguard

import (
	"os"
	"path/filepath"
	"testing"
)

// The sweep's only defence against killing a stranger's process is the
// executable path, so these tests are written against a fake process table:
// they are about the decision, not about anything a Mac has to be present
// for. They run on every platform for the same reason.

const (
	bundleHelpers = "/Applications/SpeechKit.app/Contents/Helpers"
	ourWhisper    = bundleHelpers + "/whisper-server"
)

func TestSweepOnlyMatchesProcessesInsideOurBundle(t *testing.T) {
	self := os.Getpid()
	processes := []Process{
		{PID: 101, ExecutablePath: ourWhisper},
		{PID: 102, ExecutablePath: bundleHelpers + "/llama/llama-server"},
		// A user's own whisper-server. Same name, not our file: sweeping it
		// would kill work that has nothing to do with SpeechKit, which is
		// exactly why the sweep never matches on the process name.
		{PID: 201, ExecutablePath: "/opt/homebrew/bin/whisper-server"},
		{PID: 202, ExecutablePath: "/Users/someone/src/whisper.cpp/build/bin/whisper-server"},
		// A second application whose bundle name merely starts the same way.
		{PID: 203, ExecutablePath: "/Applications/SpeechKit.app.backup/Contents/Helpers/whisper-server"},
		// A sibling directory inside our own bundle: still not the root the
		// host named, so still not swept.
		{PID: 204, ExecutablePath: "/Applications/SpeechKit.app/Contents/MacOS/SpeechKit"},
		// Escapes back out of the root.
		{PID: 205, ExecutablePath: bundleHelpers + "/../MacOS/SpeechKit"},
	}

	stale := staleUnder(processes, bundleHelpers, self)

	got := map[int]bool{}
	for _, process := range stale {
		got[process.PID] = true
	}
	for _, pid := range []int{101, 102} {
		if !got[pid] {
			t.Errorf("pid %d is inside %s and was not selected", pid, bundleHelpers)
		}
	}
	for _, pid := range []int{201, 202, 203, 204, 205} {
		if got[pid] {
			t.Errorf("pid %d is not ours and would have been killed", pid)
		}
	}
	if len(stale) != 2 {
		t.Fatalf("staleUnder selected %d processes, want 2: %+v", len(stale), stale)
	}
}

func TestSweepNeverSelectsItselfOrTheRootProcesses(t *testing.T) {
	self := os.Getpid()
	processes := []Process{
		{PID: self, ExecutablePath: ourWhisper},
		{PID: 0, ExecutablePath: ourWhisper},
		{PID: 1, ExecutablePath: ourWhisper},
	}
	if stale := staleUnder(processes, bundleHelpers, self); len(stale) != 0 {
		t.Fatalf("staleUnder selected %+v, want nothing", stale)
	}
}

func TestSweepSelectsNothingWithoutARoot(t *testing.T) {
	processes := []Process{{PID: 101, ExecutablePath: ourWhisper}}
	for _, root := range []string{"", "   "} {
		if stale := staleUnder(processes, root, os.Getpid()); len(stale) != 0 {
			t.Fatalf("staleUnder(root=%q) selected %+v, want nothing", root, stale)
		}
	}
}

func TestPathIsUnder(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "Applications", "SpeechKit.app", "Contents", "Helpers")
	cases := []struct {
		name      string
		candidate string
		want      bool
	}{
		{"direct child", filepath.Join(root, "whisper-server"), true},
		{"nested child", filepath.Join(root, "llama", "llama-server"), true},
		{"uncleaned child", root + string(filepath.Separator) + "." + string(filepath.Separator) + "whisper-server", true},
		{"the root itself", root, false},
		{"parent", filepath.Dir(root), false},
		{"sibling with a shared prefix", root + "-old", false},
		{"escapes upward", filepath.Join(root, "..", "MacOS", "SpeechKit"), false},
		{"empty candidate", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := pathIsUnder(root, tc.candidate); got != tc.want {
				t.Fatalf("pathIsUnder(%q, %q) = %v, want %v", root, tc.candidate, got, tc.want)
			}
		})
	}
	if pathIsUnder("", filepath.Join(root, "whisper-server")) {
		t.Fatal("pathIsUnder with an empty root reported a match")
	}
}
