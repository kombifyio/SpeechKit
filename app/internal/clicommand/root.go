package clicommand

import (
	"errors"
	"github.com/spf13/cobra"
	"io"
)

type globalOptions struct {
	server string
	token  string
	json   bool
}

type exitError struct {
	code int
	err  error
}

func (e exitError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

// Run executes the SpeechKit CLI command graph and returns its process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	var opts globalOptions
	root := newRootCommand(&opts, stdout, stderr)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		var ee exitError
		if errors.As(err, &ee) {
			if ee.err != nil {
				writeLineIgnore(stderr, ee.err)
			}
			return ee.code
		}
		writeLineIgnore(stderr, err)
		return 2
	}
	return 0
}

func newRootCommand(opts *globalOptions, stdout, stderr io.Writer) *cobra.Command {
	root := &cobra.Command{
		Use:           "speechkitctl",
		Short:         "SpeechKit server diagnostics and quick actions",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetOut(stdout)
	root.SetErr(stderr)
	root.TraverseChildren = true
	root.PersistentFlags().StringVarP(&opts.server, "server", "s", "", "SpeechKit server URL")
	root.PersistentFlags().StringVarP(&opts.token, "token", "t", "", "SpeechKit bearer token")
	root.PersistentFlags().BoolVar(&opts.json, "json", false, "write JSON output")

	root.AddCommand(newStatusCommand(opts, stdout, stderr))
	root.AddCommand(newCatalogCommand(opts, stdout, stderr))
	root.AddCommand(newConfigCommand(opts, stdout, stderr))
	root.AddCommand(newPersonasCommand(opts, stdout))
	root.AddCommand(newTranscribeCommand(opts, stdout, stderr))
	root.AddCommand(newInitCommand(opts, stdout, stderr))
	root.AddCommand(newDiscoverCommand(opts, stdout, stderr))
	return root
}

func exitFromCode(code int) error {
	if code == 0 {
		return nil
	}
	return exitError{code: code}
}
