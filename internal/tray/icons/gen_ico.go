//go:build ignore

package main

import (
	"io"
	"os"
	"path/filepath"
)

func main() {
	srcPath := filepath.Join("..", "..", "..", "assets", "speechkit.ico")
	src, err := os.Open(srcPath)
	if err != nil {
		panic(err)
	}
	defer src.Close()

	dst, err := os.Create("speechkit.ico")
	if err != nil {
		panic(err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		panic(err)
	}
}
