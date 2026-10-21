package clicommand

import (
	"context"
	"github.com/kombifyio/SpeechKit/app/internal/server/discovery"
	"github.com/spf13/cobra"
	"io"
	"time"
)

// lanBrowse is the LAN lookup used by `discover`. Tests replace it so the
// command can be exercised without multicast.
var lanBrowse = discovery.Browse

func newDiscoverCommand(opts *globalOptions, stdout, stderr io.Writer) *cobra.Command {
	timeout := 2 * time.Second
	cmd := &cobra.Command{
		Use:   "discover",
		Short: "Find SpeechKit servers on the local network",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			servers := lanBrowse(ctx)
			if opts.json {
				if servers == nil {
					servers = []discovery.Record{}
				}
				return exitFromCode(writeJSON(stdout, stderr, servers))
			}
			if len(servers) == 0 {
				return writef(stdout, "no SpeechKit servers found on the LAN\n")
			}
			for _, rec := range servers {
				if err := writef(stdout, "%s\t%s\n", rec.InstanceName, rec.URL); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 2*time.Second, "how long to wait for replies")
	return cmd
}
