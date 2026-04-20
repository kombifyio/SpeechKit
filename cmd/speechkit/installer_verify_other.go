//go:build !windows

package main

import "fmt"

func verifyInstallerSignature(path string) error {
	return fmt.Errorf("installer signature verification is only supported on Windows")
}
