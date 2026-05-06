package main

import "testing"

func TestDesktopCleanupStackRunsInReverseRegistrationOrder(t *testing.T) {
	var calls []string
	var cleanup desktopCleanupStack
	cleanup.Add(func() { calls = append(calls, "first") })
	cleanup.Add(func() { calls = append(calls, "second") })

	cleanup.Close()

	if len(calls) != 2 || calls[0] != "second" || calls[1] != "first" {
		t.Fatalf("cleanup calls = %v, want [second first]", calls)
	}
}
