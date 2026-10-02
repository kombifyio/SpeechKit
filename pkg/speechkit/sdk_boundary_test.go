package speechkit

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPublicSDKDoesNotImportAppModule walks the ENTIRE pkg/speechkit tree
// (not just the root package) and fails when any production or test file
// imports the reference-app module (github.com/kombifyio/SpeechKit/app,
// see docs/ADR/0004-sdk-module-boundary.md) or a legacy repo-internal path
// (cmd/, internal/, tools/ of the root module). The root module cannot require
// the app module without a dependency cycle, so the compiler already rejects
// most violations; this test keeps the boundary explicit and fails with a
// pointer to the documentation.
func TestPublicSDKDoesNotImportAppModule(t *testing.T) {
	const module = "github.com/kombifyio/SpeechKit"
	forbidden := []string{
		module + "/app",
		module + "/internal/",
		module + "/cmd/",
	}

	fset := token.NewFileSet()
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			for _, prefix := range forbidden {
				if importPath == prefix || strings.HasPrefix(importPath, strings.TrimSuffix(prefix, "/")+"/") {
					t.Errorf("%s imports %s; pkg/speechkit must remain externally embeddable and must not depend on the app module (see docs/architecture/sdk-surface-boundary.md)", path, importPath)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk pkg/speechkit: %v", err)
	}
}

// TestRootPackageImportsNoOpenTelemetry enforces boundary rule 7: the root
// package holds contracts and value types only, so it must not pull
// OpenTelemetry into every embedder. OTel-backed helpers live in
// pkg/speechkit/telemetry.
func TestRootPackageImportsNoOpenTelemetry(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read root package dir: %v", err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, imp := range file.Imports {
			if p := strings.Trim(imp.Path.Value, `"`); strings.HasPrefix(p, "go.opentelemetry.io/") {
				t.Errorf("%s imports %s; move OTel-dependent code to pkg/speechkit/telemetry (boundary rule 7)", name, p)
			}
		}
	}
}
