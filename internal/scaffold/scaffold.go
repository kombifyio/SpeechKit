// Package scaffold renders embedded starter templates into a target
// directory so callers can bootstrap a SpeechKit integration without
// hand-copying boilerplate. The engine is invoked from speechkitctl init
// for humans and from speechkit-mcp's read-only scaffold tools for coding
// agents; both share the same template set.
package scaffold

import (
	"bufio"
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/BurntSushi/toml"
)

//go:embed all:templates
var templatesFS embed.FS

const (
	templateRoot     = "templates"
	templateMetaFile = "template.toml"
	tmplSuffix       = ".tmpl"
)

// VarSpec describes one variable a template wants resolved before
// rendering.
type VarSpec struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Default     string `toml:"default"`
	EnvHint     string `toml:"env_hint"`
	Optional    bool   `toml:"optional"`
}

// PostInitHook is a command run after files are written. Hooks fire in
// declaration order; the first non-zero exit aborts the rest.
type PostInitHook struct {
	Description string `toml:"description"`
	Cmd         string `toml:"cmd"`
}

// TemplateMeta is the parsed template.toml.
type TemplateMeta struct {
	Name             string         `toml:"name"`
	Description      string         `toml:"description"`
	MinServerVersion string         `toml:"min_server_version"`
	Vars             []VarSpec      `toml:"vars"`
	PostInit         []PostInitHook `toml:"post_init"`
}

// ScaffoldOptions configure a Scaffold call.
type ScaffoldOptions struct {
	// Template is the directory name under templates/.
	Template string

	// OutputDir is the absolute path to write files into. If empty, the
	// engine returns generated files in Result.Files without touching disk.
	OutputDir string

	// Vars are caller-provided variable overrides. They take precedence
	// over template defaults and EnvHint lookups.
	Vars map[string]string

	// Interactive, when true, prompts on stdin for any required variable
	// not satisfied by Vars / EnvHint / Default.
	Interactive bool

	// In and Out are used for interactive prompting and progress output.
	// When nil they default to os.Stdin and os.Stdout.
	In  io.Reader
	Out io.Writer

	// RunPostInit, when true, executes PostInitHook commands after files
	// are written. Disabled by default; hosts opt in.
	RunPostInit bool
}

// GeneratedFile is one file the scaffolder produced. AbsPath is set only
// when OutputDir was configured.
type GeneratedFile struct {
	RelPath string
	AbsPath string
	Content []byte
}

// Result is returned by Scaffold and is also the response shape the MCP
// generate_integration tool serialises to JSON.
type Result struct {
	Template string
	Vars     map[string]string
	Files    []GeneratedFile
	// Hooks reports which post-init hooks were executed (nil unless
	// RunPostInit was set).
	Hooks []HookResult
}

// HookResult records the outcome of one PostInitHook execution.
type HookResult struct {
	Description string
	Cmd         string
	Success     bool
	Output      string
}

var (
	// ErrUnknownTemplate is returned when ScaffoldOptions.Template does
	// not match any embedded template directory.
	ErrUnknownTemplate = errors.New("scaffold: unknown template")

	// ErrMissingRequiredVar is returned when a non-optional VarSpec is
	// neither overridden nor satisfiable from EnvHint and Default and
	// Interactive is false.
	ErrMissingRequiredVar = errors.New("scaffold: missing required variable")
)

// FS exposes the embedded template filesystem for callers that want to
// list or inspect templates without invoking the full Scaffold pipeline
// (e.g. an MCP "list templates" tool).
func FS() fs.FS { return templatesFS }

// ListTemplates returns the metadata for every embedded template in
// alphabetical order by name.
func ListTemplates() ([]TemplateMeta, error) {
	dirEntries, err := fs.ReadDir(templatesFS, templateRoot)
	if err != nil {
		return nil, fmt.Errorf("read templates dir: %w", err)
	}
	out := make([]TemplateMeta, 0, len(dirEntries))
	for _, entry := range dirEntries {
		if !entry.IsDir() {
			continue
		}
		meta, err := loadMeta(entry.Name())
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", entry.Name(), err)
		}
		out = append(out, meta)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// LookupTemplate returns metadata for a single template, or
// ErrUnknownTemplate if the template directory does not exist.
func LookupTemplate(name string) (TemplateMeta, error) {
	if !templateExists(name) {
		return TemplateMeta{}, fmt.Errorf("%w: %s", ErrUnknownTemplate, name)
	}
	return loadMeta(name)
}

// Scaffold renders the named template with the supplied variables. When
// OutputDir is non-empty, the resulting files are written to disk and the
// returned GeneratedFile entries carry AbsPath. When OutputDir is empty
// the engine returns the rendered content in-memory only, used by the
// MCP scaffold tool to hand files back to the calling agent.
func Scaffold(opts ScaffoldOptions) (*Result, error) {
	if !templateExists(opts.Template) {
		return nil, fmt.Errorf("%w: %s", ErrUnknownTemplate, opts.Template)
	}
	meta, err := loadMeta(opts.Template)
	if err != nil {
		return nil, err
	}

	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	in := opts.In
	if in == nil {
		in = os.Stdin
	}

	resolved, err := resolveVars(meta.Vars, opts.Vars, opts.Interactive, in, out)
	if err != nil {
		return nil, err
	}

	files, err := renderTemplate(opts.Template, resolved)
	if err != nil {
		return nil, err
	}

	if opts.OutputDir != "" {
		if err := writeFiles(opts.OutputDir, files); err != nil {
			return nil, err
		}
		for i := range files {
			files[i].AbsPath = filepath.Join(opts.OutputDir, files[i].RelPath)
		}
	}

	result := &Result{
		Template: opts.Template,
		Vars:     resolved,
		Files:    files,
	}

	if opts.RunPostInit && opts.OutputDir != "" {
		hooks, err := runHooks(opts.OutputDir, meta.PostInit, out)
		result.Hooks = hooks
		if err != nil {
			return result, err
		}
	}

	return result, nil
}

func templateExists(name string) bool {
	if name == "" {
		return false
	}
	_, err := fs.Stat(templatesFS, path.Join(templateRoot, name))
	return err == nil
}

func loadMeta(name string) (TemplateMeta, error) {
	data, err := fs.ReadFile(templatesFS, path.Join(templateRoot, name, templateMetaFile))
	if err != nil {
		return TemplateMeta{}, fmt.Errorf("read %s: %w", templateMetaFile, err)
	}
	var meta TemplateMeta
	if _, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&meta); err != nil {
		return TemplateMeta{}, fmt.Errorf("decode %s: %w", templateMetaFile, err)
	}
	if meta.Name == "" {
		meta.Name = name
	}
	return meta, nil
}

func resolveVars(specs []VarSpec, overrides map[string]string, interactive bool, in io.Reader, out io.Writer) (map[string]string, error) {
	resolved := map[string]string{}
	for _, spec := range specs {
		if v, ok := overrides[spec.Name]; ok && strings.TrimSpace(v) != "" {
			resolved[spec.Name] = v
			continue
		}
		if spec.EnvHint != "" {
			if env := os.Getenv(spec.EnvHint); env != "" {
				resolved[spec.Name] = env
				continue
			}
		}
		if spec.Default != "" {
			resolved[spec.Name] = spec.Default
			continue
		}
		if spec.Optional {
			resolved[spec.Name] = ""
			continue
		}
		if interactive {
			val, err := promptVar(spec, in, out)
			if err != nil {
				return nil, err
			}
			resolved[spec.Name] = val
			continue
		}
		return nil, fmt.Errorf("%w: %s", ErrMissingRequiredVar, spec.Name)
	}
	for k, v := range overrides {
		if _, declared := resolved[k]; !declared {
			resolved[k] = v
		}
	}
	return resolved, nil
}

func promptVar(spec VarSpec, in io.Reader, out io.Writer) (string, error) {
	reader := bufio.NewReader(in)
	hint := spec.Description
	if hint == "" {
		hint = spec.Name
	}
	if spec.EnvHint != "" {
		hint = fmt.Sprintf("%s (env: %s)", hint, spec.EnvHint)
	}
	fmt.Fprintf(out, "%s: ", hint)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("read %s: %w", spec.Name, err)
	}
	value := strings.TrimRight(line, "\r\n")
	if value == "" && !spec.Optional {
		return "", fmt.Errorf("%w: %s", ErrMissingRequiredVar, spec.Name)
	}
	return value, nil
}

func renderTemplate(name string, vars map[string]string) ([]GeneratedFile, error) {
	root := path.Join(templateRoot, name)
	var files []GeneratedFile

	err := fs.WalkDir(templatesFS, root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimPrefix(p, root+"/")
		if rel == templateMetaFile {
			return nil
		}
		raw, err := fs.ReadFile(templatesFS, p)
		if err != nil {
			return fmt.Errorf("read %s: %w", p, err)
		}
		outRel, _ := strings.CutSuffix(rel, tmplSuffix)
		// Render the relative path itself through templating so callers
		// can move files based on variables (e.g. {{ .APP_NAME }}/foo).
		renderedRel, err := renderString(outRel, vars)
		if err != nil {
			return fmt.Errorf("render path %s: %w", outRel, err)
		}
		var content []byte
		if strings.HasSuffix(rel, tmplSuffix) {
			rendered, err := renderString(string(raw), vars)
			if err != nil {
				return fmt.Errorf("render %s: %w", rel, err)
			}
			content = []byte(rendered)
		} else {
			content = raw
		}
		files = append(files, GeneratedFile{
			RelPath: filepath.FromSlash(renderedRel),
			Content: content,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool { return files[i].RelPath < files[j].RelPath })
	return files, nil
}

func renderString(text string, vars map[string]string) (string, error) {
	tmpl, err := template.New("scaffold").
		Option("missingkey=error").
		Parse(text)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func writeFiles(outputDir string, files []GeneratedFile) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", outputDir, err)
	}
	cleanRoot := filepath.Clean(outputDir)
	for _, f := range files {
		target, err := SafeJoin(cleanRoot, f.RelPath)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(target), err)
		}
		if err := os.WriteFile(target, f.Content, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", target, err)
		}
	}
	return nil
}

// SafeJoin joins root + rel and rejects any path that would escape root.
// Exposed for the MCP scaffold handler so it can validate output_dir
// before writing.
func SafeJoin(root, rel string) (string, error) {
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("scaffold: absolute path %q escapes output dir", rel)
	}
	root = filepath.Clean(root)
	target := filepath.Join(root, filepath.Clean(rel))
	if target != root && !strings.HasPrefix(target, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("scaffold: path %q escapes output dir", rel)
	}
	return target, nil
}
