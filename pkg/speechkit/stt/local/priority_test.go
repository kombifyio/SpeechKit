package local

import "testing"

func TestSubprocessPriorityInstanceOverrideWinsOverGlobal(t *testing.T) {
	SetSubprocessPriorityLowered(false)
	t.Cleanup(func() { SetSubprocessPriorityLowered(true) })

	lowered := true
	if !subprocessPriorityLowered(&lowered) {
		t.Fatal("instance override true must win over global false")
	}
	if subprocessPriorityLowered(nil) {
		t.Fatal("without override the global default applies")
	}
}
