//go:build !cgo

// speechkit-stt-sherpa needs the sherpa-onnx native library, so a build
// without cgo only reports that and exits with the bad-configuration code.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "speechkit-stt-sherpa requires a cgo build with the sherpa-onnx runtime (CGO_ENABLED=1)")
	os.Exit(3)
}
