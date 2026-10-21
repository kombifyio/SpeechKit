package clicommand

import (
	"context"
	skclient "github.com/kombifyio/SpeechKit/pkg/speechkit/x/client"
	"github.com/spf13/cobra"
	"io"
)

func newTranscribeCommand(opts *globalOptions, stdout, stderr io.Writer) *cobra.Command {
	var language, model string
	cmd := &cobra.Command{
		Use:   "transcribe <file>",
		Short: "Transcribe an audio file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(opts, func(ctx context.Context, c *skclient.Client) error {
				result, err := c.TranscribeFile(ctx, args[0], skclient.TranscribeOptions{Language: language, Model: model})
				if err != nil {
					return exitError{code: 1, err: err}
				}
				if opts.json {
					return exitFromCode(writeJSON(stdout, stderr, result))
				}
				return writeLine(stdout, result.Text)
			})
		},
	}
	cmd.Flags().StringVar(&language, "language", "", "language hint")
	cmd.Flags().StringVar(&model, "model", "", "model hint")
	return cmd
}
