//go:build !no_embed_frontend

package frontendassets

import (
	"embed"
	"io/fs"
)

// dist is the Vite-built reference UI. The directory is gitignored
// (see f4dea1b) — it is materialised by `npm run build` before the
// canonical Wails build runs. CI jobs that don't build the frontend
// (Go Analysis, server-linux) compile this package under the
// `no_embed_frontend` build tag, which selects the stub in
// assets_stub.go and avoids the //go:embed pattern's "no matching
// files found" failure on a clean checkout.
//
//go:embed dist
var embedded embed.FS

func Files() fs.FS {
	dist, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return dist
}
