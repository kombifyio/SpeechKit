//go:build no_embed_frontend

// Package frontendassets stub — selected when the caller cannot or
// does not want to embed the Vite build artefacts. Used by CI jobs
// that lint/vet/test the kernel without first running `npm run build`
// to populate internal/frontendassets/dist (which is gitignored).
//
// Production binaries (cmd/speechkit Wails desktop bundle) compile
// without this tag so the real //go:embed in assets.go takes effect.
package frontendassets

import (
	"io/fs"
	"testing/fstest"
)

func Files() fs.FS {
	return fstest.MapFS{}
}
