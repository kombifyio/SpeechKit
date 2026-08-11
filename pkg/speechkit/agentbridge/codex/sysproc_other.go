//go:build !windows

package codex

import "os/exec"

func configureSysProcAttr(_ *exec.Cmd) {}
