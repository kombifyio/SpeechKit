//go:build windows

package stt

import (
	"os/exec"
	"testing"
)

func TestConfigureHiddenProcessSetsWindowsFlags(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "echo", "ok")

	configureHiddenProcess(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("expected SysProcAttr to be configured")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("expected HideWindow to be true")
	}
	if cmd.SysProcAttr.CreationFlags != createNoWindow {
		t.Fatalf("CreationFlags = %#x, want %#x", cmd.SysProcAttr.CreationFlags, createNoWindow)
	}
}
