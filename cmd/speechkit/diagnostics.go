package main

import (
	"archive/zip"
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

type diagnosticsBundleOptions struct {
	OutPath            string
	IncludeTranscripts bool
	SinceDays          int    // default 7
	LogsDir            string // override for tests
	ConfigPath         string // override for tests
	SBOMPath           string // override for tests
}

const defaultSinceDays = 7

// secretPattern matches likely secrets in log lines. Two forms are handled:
//
//  1. key=value or key: value (TOML, env, structured log): captures the key+separator
//     as group 1 so the replacement preserves it.
//  2. Bearer <token> in HTTP Authorization headers (no = or :): captured as group 1.
//
// JSON fields with quoted keys ("api_key":"secret") are also matched because
// the JSON double-quotes are treated as surrounding whitespace by the outer
// character class.
var secretPattern = regexp.MustCompile(
	`(?i)` +
		// Form 1: keyword followed by optional whitespace, then = or :, then the value.
		// Handles TOML (api_key = "val"), env (API_KEY=val), JSON ("api_key":"val").
		`(["']?(?:api[-_]?key|x-api-key|token|password|secret|credential)["']?\s*[:=]\s*)["']?[^\s,"'{}]+["']?` +
		`|` +
		// Form 2: Bearer <token> in HTTP Authorization headers (value after a space).
		`(Bearer\s+)\S+`,
)

// transcriptPattern matches audit-log JSON fields that contain transcript text.
// Covers the most likely field names: transcript_text, transcript, text.
var transcriptPattern = regexp.MustCompile(
	`("(?:transcript_text|transcript|transcribed_text)"\s*:\s*)"[^"]*"`,
)

// redactLine masks likely secrets in a single log line.
// The replacement callback preserves whichever group matched (Form 1 or Form 2).
func redactLine(s string) string {
	return secretPattern.ReplaceAllStringFunc(s, func(match string) string {
		subs := secretPattern.FindStringSubmatch(match)
		if len(subs) >= 3 && subs[1] != "" {
			// Form 1: key=value — preserve the key+separator.
			return subs[1] + "<redacted>"
		}
		if len(subs) >= 3 && subs[2] != "" {
			// Form 2: Bearer <token> — preserve "Bearer ".
			return subs[2] + "<redacted>"
		}
		return "<redacted>"
	})
}

// redactTranscripts masks transcript content in JSON log lines.
func redactTranscripts(s string) string {
	return transcriptPattern.ReplaceAllString(s, `${1}"<redacted>"`)
}

// collectSupportBundle writes a ZIP at opts.OutPath containing redacted
// logs, redacted config, system info, and SBOM if available.
func collectSupportBundle(opts diagnosticsBundleOptions) error {
	if opts.SinceDays == 0 {
		opts.SinceDays = defaultSinceDays
	}
	out, err := os.Create(opts.OutPath) //#nosec G304 -- path comes from CLI arg, user intent
	if err != nil {
		return fmt.Errorf("create %s: %w", opts.OutPath, err)
	}
	defer func() { _ = out.Close() }()

	zw := zip.NewWriter(out)
	defer func() { _ = zw.Close() }()

	if err := writeBundleReadme(zw, opts); err != nil {
		return fmt.Errorf("readme: %w", err)
	}
	if err := writeBundleLogs(zw, opts); err != nil {
		return fmt.Errorf("logs: %w", err)
	}
	if err := writeBundleConfig(zw, opts); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := writeBundleSystem(zw); err != nil {
		return fmt.Errorf("system: %w", err)
	}
	// SBOM is optional — missing file is not an error.
	if err := writeBundleSBOM(zw, opts); err != nil {
		fmt.Fprintln(os.Stderr, "warn: SBOM not included:", err)
	}
	return nil
}

func writeBundleReadme(zw *zip.Writer, opts diagnosticsBundleOptions) error {
	f, err := zw.Create("README.md")
	if err != nil {
		return err
	}
	content := fmt.Sprintf(`# SpeechKit Support Bundle

Generated: %s
Since-days: %d
Include-transcripts: %v

## Contents

- logs/         — runtime and audit logs (last %d days). Secrets redacted.
- config/       — config.toml with provider keys + tokens replaced by <redacted>.
- system/       — OS, hardware, environment (no PATH, no system secrets).
- sbom/         — Software Bill of Materials if available.

## How to use

Attach the entire ZIP to your support ticket. The bundle is safe to
send: no provider keys, no full transcripts (unless --include-transcripts
was used), no system PATH.

If you used --include-transcripts, review the audit.log before sending.
`, time.Now().UTC().Format(time.RFC3339), opts.SinceDays, opts.IncludeTranscripts, opts.SinceDays)
	_, err = f.Write([]byte(content))
	return err
}

func writeBundleLogs(zw *zip.Writer, opts diagnosticsBundleOptions) error {
	logsDir := opts.LogsDir
	if logsDir == "" {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("resolve exe path: %w", err)
		}
		logsDir = filepath.Join(filepath.Dir(exePath), "logs")
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -opts.SinceDays)

	entries, err := os.ReadDir(logsDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // no logs dir is fine
		}
		return fmt.Errorf("read logs dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		// Only include speechkit*.log and audit-*.log
		if !strings.HasPrefix(name, "speechkit") && !strings.HasPrefix(name, "audit-") {
			continue
		}
		srcPath := filepath.Join(logsDir, name)
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			continue
		}
		if err := copyLogIntoZip(zw, srcPath, "logs/"+name, opts.IncludeTranscripts); err != nil {
			fmt.Fprintln(os.Stderr, "warn: skipping log", name, ":", err)
		}
	}
	return nil
}

func copyLogIntoZip(zw *zip.Writer, srcPath, zipPath string, includeTranscripts bool) error {
	src, err := os.Open(srcPath) //#nosec G304 -- srcPath is composed under logsDir which is under exe dir
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	f, err := zw.Create(zipPath)
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(src)
	// Support large JSON audit-log lines (up to 4 MiB per line).
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		line = redactLine(line)
		if !includeTranscripts {
			line = redactTranscripts(line)
		}
		if _, err := io.WriteString(f, line+"\n"); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func writeBundleConfig(zw *zip.Writer, opts diagnosticsBundleOptions) error {
	configPath := opts.ConfigPath
	if configPath == "" {
		exePath, _ := os.Executable()
		configPath = filepath.Join(filepath.Dir(exePath), "config.toml")
	}
	src, err := os.Open(configPath) //#nosec G304 -- path is under exe dir
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // no config file is fine
		}
		return err
	}
	defer func() { _ = src.Close() }()

	f, err := zw.Create("config/config.toml.redacted")
	if err != nil {
		return err
	}

	scanner := bufio.NewScanner(src)
	for scanner.Scan() {
		if _, err := io.WriteString(f, redactLine(scanner.Text())+"\n"); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func writeBundleSystem(zw *zip.Writer) error {
	f, err := zw.Create("system/info.txt")
	if err != nil {
		return err
	}

	fmt.Fprintf(f, "OS: %s\n", runtime.GOOS)
	fmt.Fprintf(f, "ARCH: %s\n", runtime.GOARCH)
	fmt.Fprintf(f, "Go: %s\n", runtime.Version())
	fmt.Fprintf(f, "NumCPU: %d\n", runtime.NumCPU())

	// Whitelist of SpeechKit-relevant env vars; exclude PATH and system secrets.
	// DSN/key/token vars print presence only, not value.
	type envSpec struct {
		key    string
		redact bool // if true, only emit "<set, redacted>" vs actual value
	}
	whitelist := []envSpec{
		{"LOCALAPPDATA", false},
		{"PROGRAMDATA", false},
		{"PROGRAMFILES", false},
		{"PROGRAMFILES(X86)", false},
		{"APPDATA", false},
		{"USERPROFILE", false},
		{"SPEECHKIT_TEST_POSTGRES_DSN", true},
		{"SPEECHKIT_CONFIG", false},
		{"SPEECHKIT_LOG_DIR", false},
		{"HF_TOKEN", true},
		{"GOOGLE_AI_API_KEY", true},
		{"OPENAI_API_KEY", true},
		{"GROQ_API_KEY", true},
	}

	fmt.Fprintln(f, "\nRelevant environment:")
	for _, spec := range whitelist {
		v := os.Getenv(spec.key)
		if v == "" {
			fmt.Fprintf(f, "  %s: <unset>\n", spec.key)
		} else if spec.redact {
			fmt.Fprintf(f, "  %s: <set, redacted>\n", spec.key)
		} else {
			fmt.Fprintf(f, "  %s: %s\n", spec.key, v)
		}
	}

	return nil
}

func writeBundleSBOM(zw *zip.Writer, opts diagnosticsBundleOptions) error {
	sbomPath := opts.SBOMPath
	if sbomPath == "" {
		exePath, _ := os.Executable()
		sbomPath = filepath.Join(filepath.Dir(exePath), "SpeechKit.sbom.json")
	}
	data, err := os.ReadFile(sbomPath) //#nosec G304 -- path under exe dir
	if err != nil {
		return err
	}
	f, err := zw.Create("sbom/SpeechKit.sbom.json")
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	return err
}
