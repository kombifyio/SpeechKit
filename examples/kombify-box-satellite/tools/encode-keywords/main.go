package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kombifyio/SpeechKit/pkg/speechkit/wakeword"
)

func main() {
	tokens := flag.String("tokens", "", "path to sherpa tokens.txt")
	flag.Parse()
	if *tokens == "" || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: encode-keywords --tokens <tokens.txt> \"HEY JARVIS :2.0 @hey_jarvis\"")
		os.Exit(2)
	}

	lines, err := wakeword.EncodeKeywords(*tokens, flag.Args())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, line := range lines {
		fmt.Println(line)
	}
}
