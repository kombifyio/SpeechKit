//go:build !(windows && cgo)

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "record-mic laeuft nur auf Windows mit CGO_ENABLED=1 (malgo).")
	os.Exit(2)
}
