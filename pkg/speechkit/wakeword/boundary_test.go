package wakeword

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRootAndTrainingLinkNoNativeEngine pins the reason the desktop host may
// import this package: neither the root package nor wakeword/training may
// import a native keyword-spotting binding. Engines live in subpackages such
// as wakeword/sherpa and register themselves; a host that must not link
// sherpa-onnx (the Wails desktop process) imports only the root.
func TestRootAndTrainingLinkNoNativeEngine(t *testing.T) {
	forbidden := []string{"sherpa-onnx-go", "onnxruntime_go"}
	fset := token.NewFileSet()
	for _, dir := range []string{".", "training"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			file, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			for _, imp := range file.Imports {
				importPath := strings.Trim(imp.Path.Value, `"`)
				for _, needle := range forbidden {
					if strings.Contains(importPath, needle) {
						t.Errorf("%s imports %s; the engine-free root must not link a native keyword-spotting engine (put it behind wakeword.RegisterEngine in a subpackage such as wakeword/sherpa)", path, importPath)
					}
				}
			}
		}
	}
}
