package main

import (
	"fmt"
	"os"

	"github.com/kombifyio/SpeechKit/app/internal/clicommand"
	"github.com/kombifyio/SpeechKit/app/internal/productid"
)

func main() {
	if err := productid.Register(productid.Reference); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(clicommand.Run(os.Args[1:], os.Stdout, os.Stderr))
}
