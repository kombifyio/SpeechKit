//go:build !windows

package secrets

import "os/exec"

func configureDopplerCommand(cmd *exec.Cmd) {
}
