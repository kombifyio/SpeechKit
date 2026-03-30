package frontendassets

import (
	"embed"
	"io/fs"
)

//go:embed dist
var embedded embed.FS

func Files() fs.FS {
	dist, err := fs.Sub(embedded, "dist")
	if err != nil {
		panic(err)
	}
	return dist
}
