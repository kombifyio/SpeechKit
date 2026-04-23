//go:build !windows

package localllm

import "os/exec"

func configureHiddenProcess(cmd *exec.Cmd) {
	_ = cmd
}
