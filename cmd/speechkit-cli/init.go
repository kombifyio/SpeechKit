package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kombifyio/SpeechKit/internal/scaffold"
)

func newInitCommand(opts *globalOptions, stdout, stderr io.Writer) *cobra.Command {
	var (
		templateName string
		listOnly     bool
		runInstall   bool
		varOverrides []string
	)

	cmd := &cobra.Command{
		Use:   "init [output-dir]",
		Short: "Scaffold a SpeechKit integration from an embedded starter template",
		Long: `Scaffold a SpeechKit integration project.

Examples:
  speechkit-cli init --list
  speechkit-cli init --template browser-dictation-react my-app
  speechkit-cli init --template browser-dictation-react my-app --install
  speechkit-cli init --template browser-dictation-react my-app --var SPEECHKIT_TOKEN=xyz`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if listOnly {
				return runInitList(stdout, stderr, opts.json)
			}
			if templateName == "" {
				return exitError{code: 2, err: fmt.Errorf("--template is required (use --list to see options)")}
			}
			outputDir := ""
			appName := ""
			if len(args) == 1 {
				outputDir = args[0]
				appName = filepath.Base(outputDir)
			}
			return runInitScaffold(stdout, stderr, templateName, outputDir, appName, varOverrides, runInstall)
		},
	}
	cmd.Flags().StringVar(&templateName, "template", "", "starter template to render")
	cmd.Flags().BoolVar(&listOnly, "list", false, "list available templates and exit")
	cmd.Flags().BoolVar(&runInstall, "install", false, "run post-init hooks such as npm install after scaffolding")
	cmd.Flags().StringArrayVar(&varOverrides, "var", nil, "override a template variable as KEY=VALUE")
	return cmd
}

func runInitList(stdout, stderr io.Writer, asJSON bool) error {
	templates, err := scaffold.ListTemplates()
	if err != nil {
		return exitError{code: 1, err: err}
	}
	if asJSON {
		return exitFromCode(writeJSON(stdout, stderr, map[string]any{"templates": templates}))
	}
	if len(templates) == 0 {
		fmt.Fprintln(stdout, "no embedded templates found")
		return nil
	}
	fmt.Fprintln(stdout, "Available SpeechKit starter templates:")
	for _, tpl := range templates {
		fmt.Fprintf(stdout, "  %s\n      %s\n", tpl.Name, tpl.Description)
	}
	return nil
}

func runInitScaffold(stdout, stderr io.Writer, templateName, outputDir, appName string, varOverrides []string, runInstall bool) error {
	overrides := map[string]string{}
	if appName != "" {
		overrides["APP_NAME"] = appName
	}
	for _, raw := range varOverrides {
		key, value, ok := strings.Cut(raw, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return exitError{code: 2, err: fmt.Errorf("--var must be KEY=VALUE, got %q", raw)}
		}
		overrides[key] = value
	}
	if outputDir != "" {
		abs, err := filepath.Abs(outputDir)
		if err != nil {
			return exitError{code: 2, err: fmt.Errorf("resolve output dir: %w", err)}
		}
		outputDir = abs
		if entries, _ := os.ReadDir(outputDir); len(entries) > 0 {
			return exitError{code: 1, err: fmt.Errorf("output dir %s already exists and is not empty", outputDir)}
		}
	}

	result, err := scaffold.Scaffold(scaffold.ScaffoldOptions{
		Template:    templateName,
		OutputDir:   outputDir,
		Vars:        overrides,
		Interactive: outputDir != "" && os.Getenv("SPEECHKIT_INIT_NONINTERACTIVE") == "",
		In:          os.Stdin,
		Out:         stdout,
		RunPostInit: runInstall,
	})
	if err != nil {
		return exitError{code: 1, err: err}
	}

	if outputDir == "" {
		for _, file := range result.Files {
			fmt.Fprintf(stdout, "---- %s ----\n%s\n", file.RelPath, string(file.Content))
		}
		return nil
	}

	fmt.Fprintf(stdout, "Scaffolded %s into %s (%d files).\n", result.Template, outputDir, len(result.Files))
	if !runInstall {
		fmt.Fprintln(stdout, "Next steps:")
		fmt.Fprintf(stdout, "  cd %s\n", outputDir)
		fmt.Fprintln(stdout, "  npm install && npm run dev")
	}
	for _, hook := range result.Hooks {
		status := "ok"
		if !hook.Success {
			status = "FAILED"
		}
		fmt.Fprintf(stderr, "[hook %s] %s\n", status, hook.Cmd)
	}
	return nil
}
