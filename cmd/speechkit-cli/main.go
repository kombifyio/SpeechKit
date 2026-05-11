package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	skclient "github.com/kombifyio/SpeechKit/pkg/speechkit/client"
)

type globalOptions struct {
	server string
	token  string
	json   bool
}

type fileConfig struct {
	ServerURL string `toml:"server_url"`
	Token     string `toml:"token"`
	Server    struct {
		URL   string `toml:"url"`
		Token string `toml:"token"`
	} `toml:"server"`
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

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
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
	return root
}

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

func withClient(opts *globalOptions, fn func(context.Context, *skclient.Client) error) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c, err := newClient(*opts)
	if err != nil {
		return exitError{code: 2, err: err}
	}
	return fn(ctx, c)
}

func exitFromCode(code int) error {
	if code == 0 {
		return nil
	}
	return exitError{code: code}
}

func newClient(opts globalOptions) (*skclient.Client, error) {
	cfg := loadFileConfig()
	server := firstNonEmpty(opts.server, os.Getenv("SPEECHKIT_SERVER_URL"), cfg.Server.URL, cfg.ServerURL)
	token := firstNonEmpty(opts.token, os.Getenv("SPEECHKIT_TOKEN"), os.Getenv("SPEECHKIT_SERVER_TOKEN"), cfg.Server.Token, cfg.Token)
	return skclient.New(skclient.Options{BaseURL: server, Token: token, UserAgent: "speechkitctl/0.1"})
}

func loadFileConfig() fileConfig {
	var cfg fileConfig
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return cfg
	}
	path := filepath.Join(home, ".config", "speechkit", "config.toml")
	if _, err := os.Stat(path); err != nil {
		return cfg
	}
	_, _ = toml.DecodeFile(path, &cfg)
	return cfg
}

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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
