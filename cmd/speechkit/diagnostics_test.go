package main

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCollectSupportBundleProducesValidZip verifies that collectSupportBundle
// produces a valid ZIP containing all expected entries and that secrets and
// transcripts are redacted by default.
func TestCollectSupportBundleProducesValidZip(t *testing.T) {
	tmp := t.TempDir()

	// Seed: logs dir with a runtime log containing a secret.
	logsDir := filepath.Join(tmp, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("mkdir logsDir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(logsDir, "speechkit.log"),
		[]byte(`{"level":"info","msg":"started","api_key":"sk-secret123"}`+"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write speechkit.log: %v", err)
	}

	auditName := "audit-" + time.Now().UTC().Format("2006-01-02") + ".log"
	if err := os.WriteFile(
		filepath.Join(logsDir, auditName),
		[]byte(`{"event":"voiceagent.session.start","transcript_text":"hello world","session_id":"abc"}`+"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write audit log: %v", err)
	}

	// Seed: config.toml with a secret.
	configPath := filepath.Join(tmp, "config.toml")
	if err := os.WriteFile(
		configPath,
		[]byte("[providers.openai]\napi_key = \"sk-secret-real\"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	// Seed: SBOM file.
	sbomPath := filepath.Join(tmp, "SpeechKit.sbom.json")
	if err := os.WriteFile(sbomPath, []byte(`{"bomFormat":"CycloneDX"}`), 0o600); err != nil {
		t.Fatalf("write sbom: %v", err)
	}

	out := filepath.Join(tmp, "bundle.zip")
	err := collectSupportBundle(diagnosticsBundleOptions{
		OutPath:    out,
		LogsDir:    logsDir,
		ConfigPath: configPath,
		SBOMPath:   sbomPath,
		SinceDays:  1,
	})
	if err != nil {
		t.Fatalf("collectSupportBundle: %v", err)
	}

	// Bundle must exist and be non-empty.
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat bundle: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatal("bundle is empty")
	}

	r, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()

	needed := map[string]bool{
		"README.md":                   false,
		"logs/speechkit.log":          false,
		"logs/" + auditName:           false,
		"config/config.toml.redacted": false,
		"system/info.txt":             false,
		"sbom/SpeechKit.sbom.json":    false,
	}

	contents := map[string]string{}
	for _, f := range r.File {
		if _, ok := needed[f.Name]; ok {
			needed[f.Name] = true
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open zip entry %s: %v", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("read zip entry %s: %v", f.Name, err)
		}
		contents[f.Name] = string(b)
	}

	for path, found := range needed {
		if !found {
			t.Errorf("expected %s in bundle, not found", path)
		}
	}

	// Runtime log: raw API key must be gone, <redacted> must be present.
	logContent := contents["logs/speechkit.log"]
	if strings.Contains(logContent, "sk-secret123") {
		t.Errorf("runtime log still contains raw secret; content: %s", logContent)
	}
	if !strings.Contains(logContent, "<redacted>") {
		t.Errorf("runtime log missing <redacted> marker; content: %s", logContent)
	}

	// Config: raw API key must be gone.
	configContent := contents["config/config.toml.redacted"]
	if strings.Contains(configContent, "sk-secret-real") {
		t.Errorf("config content still contains raw api_key; content: %s", configContent)
	}
	if !strings.Contains(configContent, "<redacted>") {
		t.Errorf("config content missing <redacted> marker; content: %s", configContent)
	}

	// Audit log: transcript text must be redacted by default.
	auditContent := contents["logs/"+auditName]
	if strings.Contains(auditContent, "hello world") {
		t.Errorf("audit log still contains transcript text (default should redact); content: %s", auditContent)
	}
	if !strings.Contains(auditContent, "<redacted>") {
		t.Errorf("audit log missing <redacted> marker for transcript; content: %s", auditContent)
	}

	// SBOM: content must be preserved verbatim.
	sbomContent := contents["sbom/SpeechKit.sbom.json"]
	if !strings.Contains(sbomContent, "CycloneDX") {
		t.Errorf("SBOM content corrupted; got: %s", sbomContent)
	}
}

// TestCollectSupportBundleIncludeTranscripts verifies that --include-transcripts
// leaves transcript fields in place.
func TestCollectSupportBundleIncludeTranscripts(t *testing.T) {
	tmp := t.TempDir()

	logsDir := filepath.Join(tmp, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("mkdir logsDir: %v", err)
	}
	auditName := "audit-" + time.Now().UTC().Format("2006-01-02") + ".log"
	if err := os.WriteFile(
		filepath.Join(logsDir, auditName),
		[]byte(`{"event":"dictation.complete","transcript_text":"transcribed text here"}`+"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write audit log: %v", err)
	}

	out := filepath.Join(tmp, "bundle.zip")
	err := collectSupportBundle(diagnosticsBundleOptions{
		OutPath:            out,
		LogsDir:            logsDir,
		ConfigPath:         filepath.Join(tmp, "config.toml"), // no file = no error
		IncludeTranscripts: true,
		SinceDays:          1,
	})
	if err != nil {
		t.Fatalf("collectSupportBundle: %v", err)
	}

	r, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "logs/"+auditName {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			content := string(b)
			if !strings.Contains(content, "transcribed text here") {
				t.Errorf("--include-transcripts: transcript text should be present; content: %s", content)
			}
			return
		}
	}
	t.Errorf("audit log entry not found in bundle")
}

// TestCollectSupportBundleNoLogsDir verifies that a missing logs directory
// is not treated as an error.
func TestCollectSupportBundleNoLogsDir(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "bundle.zip")

	err := collectSupportBundle(diagnosticsBundleOptions{
		OutPath:    out,
		LogsDir:    filepath.Join(tmp, "nonexistent-logs"),
		ConfigPath: filepath.Join(tmp, "nonexistent-config.toml"),
		SinceDays:  1,
	})
	if err != nil {
		t.Fatalf("missing logs dir should not error: %v", err)
	}

	r, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()

	// Must still have README and system info.
	found := map[string]bool{}
	for _, f := range r.File {
		found[f.Name] = true
	}
	for _, required := range []string{"README.md", "system/info.txt"} {
		if !found[required] {
			t.Errorf("expected %s even with missing logs dir", required)
		}
	}
}

// TestRedactLineMasksKnownPatterns verifies that the secret regex covers all
// common shapes found in SpeechKit logs and config.
func TestRedactLineMasksKnownPatterns(t *testing.T) {
	cases := []struct {
		in       string
		mustHide string // substring that must NOT appear after redaction
	}{
		{`api_key = "sk-1234567890"`, "sk-1234567890"},
		{`api-key = "sk-1234567890"`, "sk-1234567890"},
		{`x-api-key: abc.def.ghi`, "abc.def.ghi"},
		{`Bearer abc.def.ghi`, "abc.def.ghi"},
		{`password: hunter2`, "hunter2"},
		{`token=mysecret123`, "mysecret123"},
		{`my_secret=hidden`, "hidden"},
		{`credential: top-secret-val`, "top-secret-val"},
		// JSON log line with api_key field (from real speechkit.log)
		{`{"level":"info","api_key":"sk-real-key-here"}`, "sk-real-key-here"},
		// TOML line (from real config.toml)
		{`token = "hf_verylongtokenvalue"`, "hf_verylongtokenvalue"},
	}
	for _, tc := range cases {
		got := redactLine(tc.in)
		if strings.Contains(got, tc.mustHide) {
			t.Errorf("redactLine(%q) = %q; secret %q still present", tc.in, got, tc.mustHide)
		}
		if !strings.Contains(got, "<redacted>") {
			t.Errorf("redactLine(%q) = %q; missing <redacted> marker", tc.in, got)
		}
	}
}

// TestRedactLinePreservesInnocuousLines ensures non-secret lines are not mangled.
func TestRedactLinePreservesInnocuousLines(t *testing.T) {
	cases := []string{
		`level=info msg="starting up" version=1.2.3`,
		`{"level":"info","msg":"audio captured","duration_ms":320}`,
		`[providers.openai]`,
		`enabled = true`,
	}
	for _, in := range cases {
		got := redactLine(in)
		if got != in {
			t.Errorf("redactLine should leave innocuous line untouched:\n  in:  %q\n  got: %q", in, got)
		}
	}
}

// TestRedactTranscriptsHidesTranscriptText verifies all expected field name variants.
func TestRedactTranscriptsHidesTranscriptText(t *testing.T) {
	cases := []struct {
		in    string
		field string
	}{
		{`{"event":"transcribe","transcript_text":"my voice is private","other":"x"}`, "transcript_text"},
		{`{"event":"dictation.complete","transcript":"some spoken words"}`, "transcript"},
		{`{"transcribed_text":"private speech content"}`, "transcribed_text"},
	}
	for _, tc := range cases {
		got := redactTranscripts(tc.in)
		// Should not contain the actual speech content.
		if strings.Contains(got, "private") || strings.Contains(got, "my voice") || strings.Contains(got, "some spoken") {
			t.Errorf("redactTranscripts(%q) did not redact %s field: %s", tc.in, tc.field, got)
		}
		if !strings.Contains(got, "<redacted>") {
			t.Errorf("redactTranscripts(%q) missing <redacted> marker: %s", tc.in, got)
		}
	}
}

// TestRedactTranscriptsLeavesOtherFieldsAlone verifies that non-transcript
// fields are preserved even when transcript fields are redacted.
func TestRedactTranscriptsLeavesOtherFieldsAlone(t *testing.T) {
	in := `{"event":"voiceagent.session.start","transcript_text":"hello","session_id":"abc-123","provider":"gemini"}`
	got := redactTranscripts(in)
	for _, must := range []string{"voiceagent.session.start", "abc-123", "gemini"} {
		if !strings.Contains(got, must) {
			t.Errorf("redactTranscripts removed non-transcript field %q; result: %s", must, got)
		}
	}
}

// TestCollectSupportBundleOldLogsExcluded verifies that log files older than
// SinceDays are excluded from the bundle.
func TestCollectSupportBundleOldLogsExcluded(t *testing.T) {
	tmp := t.TempDir()
	logsDir := filepath.Join(tmp, "logs")
	if err := os.MkdirAll(logsDir, 0o700); err != nil {
		t.Fatalf("mkdir logsDir: %v", err)
	}

	// Write a "recent" log (today's audit log).
	recentName := "audit-" + time.Now().UTC().Format("2006-01-02") + ".log"
	if err := os.WriteFile(
		filepath.Join(logsDir, recentName),
		[]byte(`{"event":"recent"}`+"\n"),
		0o600,
	); err != nil {
		t.Fatalf("write recent log: %v", err)
	}

	// Write an "old" log and backdate its mtime to 30 days ago.
	oldName := "speechkit-20000101-000000.log"
	oldPath := filepath.Join(logsDir, oldName)
	if err := os.WriteFile(oldPath, []byte(`{"event":"old"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write old log: %v", err)
	}
	oldTime := time.Now().AddDate(0, 0, -30)
	if err := os.Chtimes(oldPath, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	out := filepath.Join(tmp, "bundle.zip")
	if err := collectSupportBundle(diagnosticsBundleOptions{
		OutPath:    out,
		LogsDir:    logsDir,
		ConfigPath: filepath.Join(tmp, "config.toml"),
		SinceDays:  7,
	}); err != nil {
		t.Fatalf("collectSupportBundle: %v", err)
	}

	r, err := zip.OpenReader(out)
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	defer r.Close()

	for _, f := range r.File {
		if f.Name == "logs/"+oldName {
			t.Errorf("old log %s should have been excluded from bundle", oldName)
		}
	}

	// Recent log must be present.
	found := false
	for _, f := range r.File {
		if f.Name == "logs/"+recentName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("recent log %s should be in bundle", recentName)
	}
}
