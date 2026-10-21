//go:build windows

package procguard

import (
	"os/exec"
	"testing"
)

func TestConfigureHiddenProcessHidesWindowAndHonoursPriorityChoice(t *testing.T) {
	for _, tc := range []struct {
		lowered bool
		want    uint32
	}{
		{true, createNoWindow | belowNormalPriorityClass},
		{false, createNoWindow},
	} {
		cmd := exec.Command("cmd.exe", "/c", "echo", "ok")
		ConfigureHiddenProcess(cmd, tc.lowered)
		if cmd.SysProcAttr == nil || !cmd.SysProcAttr.HideWindow {
			t.Fatalf("lowered=%v: window not hidden", tc.lowered)
		}
		if cmd.SysProcAttr.CreationFlags != tc.want {
			t.Fatalf("lowered=%v: CreationFlags = %#x, want %#x", tc.lowered, cmd.SysProcAttr.CreationFlags, tc.want)
		}
	}
}
