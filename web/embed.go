// Package web embeds the built dashboard (web/dist) into the server binary.
// Only dist/.gitkeep is committed — enough for go:embed to compile on a
// fresh clone; `npm run build` fills dist/ before a release build.
package web

import (
	"embed"
	"errors"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// ErrNotBuilt means the server was compiled without running the web build.
var ErrNotBuilt = errors.New("dashboard not built: run `npm ci && npm run build` in web/, then rebuild the server")

// Dist returns the built dashboard rooted at its index.html.
func Dist() (fs.FS, error) {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil, err
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil, ErrNotBuilt
	}
	return sub, nil
}
