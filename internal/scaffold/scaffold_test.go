package scaffold

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestListTemplates_IncludesEmbeddedTemplates(t *testing.T) {
	templates, err := ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(templates) == 0 {
		t.Fatalf("expected at least one embedded template")
	}
	required := map[string]bool{
		"browser-dictation-react":   false,
		"go-assist-voice-companion": false,
		"go-voice-agent-companion":  false,
		"go-dictation-handsfree-ui": false,
	}
	for _, tpl := range templates {
		if _, ok := required[tpl.Name]; ok {
			required[tpl.Name] = true
			if tpl.Description == "" {
				t.Errorf("%s missing description", tpl.Name)
			}
		}
	}
	for name, found := range required {
		if found {
			continue
		}
		names := make([]string, 0, len(templates))
		for _, tpl := range templates {
			names = append(names, tpl.Name)
		}
		t.Fatalf("%s not found among %v", name, names)
	}
}

func TestListTemplates_SkipsIncompleteTemplateDirectories(t *testing.T) {
	templates, err := ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	incomplete := map[string]bool{
		"browser-voiceagent-svelte": true,
		"go-server-embed":           true,
		"node-voice-agent-tools":    true,
	}
	for _, tpl := range templates {
		if incomplete[tpl.Name] {
			t.Fatalf("ListTemplates exposed incomplete template %q", tpl.Name)
		}
	}
}

func TestLookupTemplate_UnknownReturnsSentinel(t *testing.T) {
	_, err := LookupTemplate("does-not-exist")
	if !errors.Is(err, ErrUnknownTemplate) {
		t.Fatalf("LookupTemplate err = %v, want ErrUnknownTemplate", err)
	}
}

func TestLookupTemplate_IncompleteTemplateReturnsSentinel(t *testing.T) {
	_, err := LookupTemplate("browser-voiceagent-svelte")
	if !errors.Is(err, ErrUnknownTemplate) {
		t.Fatalf("LookupTemplate err = %v, want ErrUnknownTemplate", err)
	}
}

func TestScaffold_RendersFilesIntoOutputDir(t *testing.T) {
	dir := t.TempDir()

	result, err := Scaffold(ScaffoldOptions{
		Template:  "browser-dictation-react",
		OutputDir: dir,
		Vars: map[string]string{
			"APP_NAME":             "my-test-app",
			"SPEECHKIT_SERVER_URL": "http://localhost:8080",
		},
	})
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if result.Template != "browser-dictation-react" {
		t.Errorf("Template = %q", result.Template)
	}
	if len(result.Files) < 6 {
		t.Errorf("expected >=6 generated files, got %d", len(result.Files))
	}

	// package.json should exist with substituted name
	pkgPath := filepath.Join(dir, "package.json")
	pkgContent, err := os.ReadFile(pkgPath)
	if err != nil {
		t.Fatalf("read package.json: %v", err)
	}
	if !strings.Contains(string(pkgContent), `"name": "my-test-app"`) {
		t.Errorf("package.json missing substituted APP_NAME, got: %s", pkgContent)
	}

	// .env.example should hold the configured server URL
	envContent, err := os.ReadFile(filepath.Join(dir, ".env.example"))
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	if !strings.Contains(string(envContent), "VITE_SPEECHKIT_SERVER_URL=http://localhost:8080") {
		t.Errorf(".env.example missing server url, got: %s", envContent)
	}

	// .tmpl suffix must be stripped
	for _, file := range result.Files {
		if strings.HasSuffix(file.RelPath, ".tmpl") {
			t.Errorf("rendered file still has .tmpl suffix: %s", file.RelPath)
		}
	}

	// template.toml metadata is never written into the output
	if _, err := os.Stat(filepath.Join(dir, "template.toml")); !os.IsNotExist(err) {
		t.Errorf("template.toml should not be in output, stat err = %v", err)
	}

	// .gitignore is copied verbatim (no .tmpl suffix on input)
	giContent, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	if !strings.Contains(string(giContent), "node_modules") {
		t.Errorf(".gitignore content unexpected: %s", giContent)
	}
}

func TestScaffold_InMemoryWhenOutputDirEmpty(t *testing.T) {
	result, err := Scaffold(ScaffoldOptions{
		Template: "browser-dictation-react",
		Vars: map[string]string{
			"APP_NAME": "demo",
		},
	})
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if len(result.Files) == 0 {
		t.Fatalf("expected files in result")
	}
	for _, f := range result.Files {
		if f.AbsPath != "" {
			t.Errorf("AbsPath should be empty when OutputDir not set, got %q", f.AbsPath)
		}
		if len(f.Content) == 0 {
			t.Errorf("file %q has empty content", f.RelPath)
		}
	}
}

func TestScaffold_GoHandsFreeTemplatesRenderPublicImports(t *testing.T) {
	for _, name := range []string{
		"go-assist-voice-companion",
		"go-voice-agent-companion",
		"go-dictation-handsfree-ui",
	} {
		t.Run(name, func(t *testing.T) {
			result, err := Scaffold(ScaffoldOptions{Template: name})
			if err != nil {
				t.Fatalf("Scaffold: %v", err)
			}
			files := map[string]string{}
			for _, file := range result.Files {
				files[file.RelPath] = string(file.Content)
			}
			if !strings.Contains(files["main.go"], "/pkg/speechkit/companion") {
				t.Fatalf("main.go does not import companion package:\n%s", files["main.go"])
			}
			if strings.Contains(files["main.go"], "/internal/") {
				t.Fatalf("main.go imports internal package:\n%s", files["main.go"])
			}
			if !strings.Contains(files["README.md"], "Hands-Free") {
				t.Fatalf("README.md should mention Hands-Free")
			}
		})
	}
}

func TestScaffold_MissingRequiredVarFails(t *testing.T) {
	_, err := Scaffold(ScaffoldOptions{
		Template: "browser-dictation-react",
		// APP_NAME omitted; not optional and no default
	})
	if !errors.Is(err, ErrMissingRequiredVar) {
		t.Fatalf("expected ErrMissingRequiredVar, got %v", err)
	}
}

func TestSafeJoin_RejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := SafeJoin(root, "../escape.txt"); err == nil {
		t.Fatalf("SafeJoin should reject ..")
	}
	if _, err := SafeJoin(root, "../../etc/passwd"); err == nil {
		t.Fatalf("SafeJoin should reject double ..")
	}
	target, err := SafeJoin(root, "nested/file.txt")
	if err != nil {
		t.Fatalf("SafeJoin valid path: %v", err)
	}
	if !strings.HasPrefix(target, root) {
		t.Errorf("SafeJoin returned %q, expected prefix %q", target, root)
	}
}

func TestScaffold_EnvHintFallback(t *testing.T) {
	t.Setenv("SPEECHKIT_SERVER_URL", "https://env.example.com")
	dir := t.TempDir()
	result, err := Scaffold(ScaffoldOptions{
		Template:  "browser-dictation-react",
		OutputDir: dir,
		Vars: map[string]string{
			"APP_NAME": "envtest",
		},
	})
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if got := result.Vars["SPEECHKIT_SERVER_URL"]; got != "https://env.example.com" {
		t.Errorf("expected env-hint fallback for SPEECHKIT_SERVER_URL, got %q", got)
	}
}

func TestScaffold_WritesRestrictiveFileModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not preserve POSIX permission bits consistently")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "app")

	_, err := Scaffold(ScaffoldOptions{
		Template:  "browser-dictation-react",
		OutputDir: dir,
		Vars: map[string]string{
			"APP_NAME": "mode-test",
		},
	})
	if err != nil {
		t.Fatalf("Scaffold: %v", err)
	}
	if info, err := os.Stat(dir); err != nil {
		t.Fatalf("stat output dir: %v", err)
	} else if got := info.Mode().Perm(); got != 0o750 {
		t.Fatalf("output dir mode = %o, want 750", got)
	}
	if info, err := os.Stat(filepath.Join(dir, "package.json")); err != nil {
		t.Fatalf("stat package.json: %v", err)
	} else if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("package.json mode = %o, want 640", got)
	}
}
