package config

import (
	"slices"
	"testing"
)

// A deny list only narrows what is captured: the per-user hive may add an
// entry, and an empty or shorter user list never removes a machine entry.
func TestUserHiveCanAddToTheScreenshotDenyListButNeverRemoveFromIt(t *testing.T) {
	machine := PolicyValues{MeetingScreenshotDeniedProcesses: []string{"KeePass.exe", "bank.exe"}}

	for _, tc := range []struct {
		name string
		user []string
		want []string
	}{
		{name: "adds", user: []string{"Signal.exe"}, want: []string{"keepass.exe", "bank.exe", "signal.exe"}},
		{name: "empty list removes nothing", user: []string{}, want: []string{"keepass.exe", "bank.exe"}},
		{name: "shorter list removes nothing", user: []string{"bank.exe"}, want: []string{"keepass.exe", "bank.exe"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := defaults()
			AttachPolicy(cfg, mergePolicyHives(machine, PolicyValues{}, PolicyValues{MeetingScreenshotDeniedProcesses: tc.user}))

			if !slices.Equal(cfg.Meeting.ScreenshotDeniedProcesses, tc.want) {
				t.Fatalf("deny list = %v, want %v", cfg.Meeting.ScreenshotDeniedProcesses, tc.want)
			}
		})
	}
}

// The deny list in force for a capture is the configured one plus the machine
// policy as it is right now, compared by file name.
func TestScreenshotDenyListJoinsTheConfiguredListAndTheMachinePolicy(t *testing.T) {
	previous := readPolicyValues
	readPolicyValues = func() PolicyValues {
		return singleSourcePolicy(PolicyValues{MeetingScreenshotDeniedProcesses: []string{`C:\Bank\Bank.EXE`}}, PolicySourceMachinePolicies)
	}
	t.Cleanup(func() { readPolicyValues = previous })

	got := ScreenshotDeniedProcesses([]string{" KeePass.exe ", ""})

	if !slices.Equal(got, []string{"keepass.exe", "bank.exe"}) {
		t.Fatalf("deny list = %v", got)
	}
}
