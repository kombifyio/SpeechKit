package clicommand

import (
	"context"
	"encoding/json"
	skclient "github.com/kombifyio/SpeechKit/pkg/speechkit/x/client"
	"github.com/spf13/cobra"
	"io"
)

func newStatusCommand(opts *globalOptions, stdout, stderr io.Writer) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show server readiness",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(opts, func(ctx context.Context, c *skclient.Client) error {
				status, err := c.Status(ctx)
				if err != nil {
					return exitError{code: 1, err: err}
				}
				if opts.json {
					return exitFromCode(writeJSON(stdout, stderr, status))
				}
				return writef(stdout, "status: %s\nversion: %s\n", status.Status, status.Version)
			})
		},
	}
}

func newCatalogCommand(opts *globalOptions, stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{Use: "catalog", Short: "Inspect provider catalog"}
	var mode string
	list := &cobra.Command{
		Use:   "list",
		Short: "List provider profiles",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(opts, func(ctx context.Context, c *skclient.Client) error {
				profiles, err := c.CatalogProfiles(ctx, mode)
				if err != nil {
					return exitError{code: 1, err: err}
				}
				if opts.json {
					return exitFromCode(writeJSON(stdout, stderr, map[string]any{"profiles": profiles}))
				}
				for _, profile := range profiles {
					if err := writef(stdout, "%s\t%s\t%s\n", profile.ID, profile.Mode, profile.Name); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
	list.Flags().StringVar(&mode, "mode", "", "mode filter: dictation, assist, or voiceagent")
	cmd.AddCommand(list)
	return cmd
}

func newConfigCommand(opts *globalOptions, stdout, stderr io.Writer) *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Inspect server config summary"}
	cmd.AddCommand(&cobra.Command{
		Use:   "get",
		Short: "Get server config summary",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(opts, func(ctx context.Context, c *skclient.Client) error {
				cfg, err := c.Config(ctx)
				if err != nil {
					return exitError{code: 1, err: err}
				}
				return exitFromCode(writeJSON(stdout, stderr, cfg))
			})
		},
	})
	return cmd
}

func newPersonasCommand(opts *globalOptions, stdout io.Writer) *cobra.Command {
	cmd := &cobra.Command{Use: "personas", Short: "Inspect Voice Agent personas"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List personas",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withClient(opts, func(ctx context.Context, c *skclient.Client) error {
				raw, err := c.Personas(ctx)
				if err != nil {
					return exitError{code: 1, err: err}
				}
				if opts.json {
					return writeLine(stdout, string(raw))
				}
				var body struct {
					Personas []struct {
						ID          string `json:"id"`
						DisplayName string `json:"displayName"`
					} `json:"personas"`
				}
				if err := json.Unmarshal(raw, &body); err != nil {
					return writeLine(stdout, string(raw))
				}
				if len(body.Personas) == 0 {
					return writeLine(stdout, string(raw))
				}
				for _, persona := range body.Personas {
					if err := writef(stdout, "%s\t%s\n", persona.ID, persona.DisplayName); err != nil {
						return err
					}
				}
				return nil
			})
		},
	})
	return cmd
}
