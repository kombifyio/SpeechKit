//go:build linux

package core

import "fmt"

func errStatus(code int) error {
	return fmt.Errorf("status %d", code)
}
