//go:build !linux

package procguard

import "os/exec"

func confinementAvailable() error   { return ErrNetworkConfinementRequired }
func startConfined(*exec.Cmd) error { return ErrNetworkConfinementRequired }
func prepareChildRead(string) error { return ErrNetworkConfinementRequired }
func adoptConfined(*exec.Cmd) error { return ErrNetworkConfinementRequired }
func shutdownConfined()             {}
