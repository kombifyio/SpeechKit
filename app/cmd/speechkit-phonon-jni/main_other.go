//go:build !(android && cgo && (amd64 || arm64))

package main

import (
	"fmt"
	"os"
)

// Outside Android the library has no JNI surface; the command unpacks an
// archive the way PhononNative.unpack does, to check it on a desktop:
//
//	go run ./app/cmd/speechkit-phonon-jni <archive.tar.zst> <dir> <name>...
func main() {
	if len(os.Args) < 4 {
		fmt.Fprintln(os.Stderr, "usage: speechkit-phonon-jni <archive.tar.zst> <dir> <name>...")
		os.Exit(2)
	}
	if err := unpack(os.Args[1], os.Args[2], os.Args[3:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
