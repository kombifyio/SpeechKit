package clicommand

import (
	"encoding/json"
	"fmt"
	"io"
)

func writeJSON(stdout, stderr io.Writer, value any) int {
	enc := json.NewEncoder(stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		writeLineIgnore(stderr, err)
		return 1
	}
	return 0
}

func writeLine(w io.Writer, values ...any) error {
	_, err := fmt.Fprintln(w, values...)
	return err
}

func writef(w io.Writer, format string, values ...any) error {
	_, err := fmt.Fprintf(w, format, values...)
	return err
}

func writeLineIgnore(w io.Writer, values ...any) {
	_, _ = fmt.Fprintln(w, values...)
}
