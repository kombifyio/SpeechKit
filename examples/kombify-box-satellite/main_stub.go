//go:build !(windows && cgo)

// Der kombify-box-Companion ist ein Windows-USB-Host (malgo/WASAPI,
// sherpa-onnx KWS, CDC-COM-Port) und braucht cgo. Dieser Stub haelt das
// Beispiel auf anderen Plattformen und in nocgo-Cross-Builds kompilierbar.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "kombify-box-satellite laeuft nur auf Windows mit CGO_ENABLED=1 (malgo + sherpa-onnx).")
	os.Exit(2)
}
