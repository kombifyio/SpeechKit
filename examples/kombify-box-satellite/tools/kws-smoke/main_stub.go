//go:build !(windows && cgo)

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "kws-smoke laeuft nur auf Windows mit CGO_ENABLED=1 (sherpa-onnx).")
	os.Exit(2)
}
