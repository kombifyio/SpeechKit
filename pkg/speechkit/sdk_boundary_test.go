package speechkit

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestPublicSDKDoesNotImportInternalPackages walks the ENTIRE pkg/speechkit
// tree (not just the root package) and fails when any production file imports
// a repo-internal package. The allowlist below names the documented adapter
// packages that may import internals as long as every exported signature uses
// public types only; a stale allowlist entry (package no longer importing that
// internal path) fails the test too, so the list can only shrink.
func TestPublicSDKDoesNotImportInternalPackages(t *testing.T) {
	const internalPrefix = "github.com/kombifyio/SpeechKit/internal/"

	// package dir (relative to pkg/speechkit) -> internal import paths that
	// package is allowed to use, each with a justification.
	allowlist := map[string]map[string]string{
		// Adapter over the voice-companion skill catalog; all internal types
		// are fully re-exported through public wrappers.
		"assist/skills": {
			internalPrefix + "assist":                        "skill catalog implementation",
			internalPrefix + "assist/skills/voice_companion": "built-in skill set",
			internalPrefix + "shortcuts":                     "shortcut actions used by skills",
		},
	}

	fset := token.NewFileSet()
	seen := map[string]map[string]bool{}

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// pkg/speechkit/internal/* is itself internal-only test tooling,
			// not public surface.
			if d.Name() == "internal" && path != "." {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		pkgDir := filepath.ToSlash(filepath.Dir(path))
		if pkgDir == "." {
			pkgDir = ""
		}
		for _, imp := range file.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(importPath, internalPrefix) {
				continue
			}
			allowed := allowlist[pkgDir]
			if allowed == nil || allowed[importPath] == "" {
				t.Errorf("%s imports %s; pkg/speechkit must remain externally embeddable (see docs/architecture/sdk-surface-boundary.md)", path, importPath)
				continue
			}
			if seen[pkgDir] == nil {
				seen[pkgDir] = map[string]bool{}
			}
			seen[pkgDir][importPath] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk pkg/speechkit: %v", err)
	}

	// Stale allowlist entries fail so the list can only shrink.
	for pkgDir, allowed := range allowlist {
		for importPath := range allowed {
			if !seen[pkgDir][importPath] {
				t.Errorf("allowlist entry stale: %s no longer imports %s — remove it", pkgDir, importPath)
			}
		}
	}
}
