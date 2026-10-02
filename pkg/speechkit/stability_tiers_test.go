package speechkit

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// stabilityLine matches the tier declaration every public package documents
// in its package comment. The release gate (CI apidiff) reads the same line to
// decide which packages are exempt from compatibility checks, so a package
// without a valid tier would silently get the wrong treatment.
var stabilityLine = regexp.MustCompile(`(?m)^Stability: (Stable|Beta|Experimental)\b`)

// TestPublicPackagesDeclareStabilityTier walks the whole pkg/speechkit tree
// and fails when a public package's doc comment has no `Stability:` line with
// one of the three tiers defined in docs/architecture/sdk-surface-boundary.md.
func TestPublicPackagesDeclareStabilityTier(t *testing.T) {
	fset := token.NewFileSet()
	documented := map[string]bool{} // package dir -> has a valid Stability line
	packages := map[string]bool{}   // every public package dir seen

	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && (d.Name() == "internal" || d.Name() == "testdata") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		dir := filepath.ToSlash(filepath.Dir(path))
		packages[dir] = true
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.PackageClauseOnly)
		if err != nil {
			return err
		}
		if file.Doc != nil && stabilityLine.MatchString(file.Doc.Text()) {
			documented[dir] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk pkg/speechkit: %v", err)
	}
	for dir := range packages {
		if !documented[dir] {
			t.Errorf("package %s: doc comment lacks a `Stability: Stable|Beta|Experimental` line (see docs/architecture/sdk-surface-boundary.md)", dir)
		}
	}
}
