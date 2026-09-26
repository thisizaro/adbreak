// Package web exposes the built frontend (web/dist) embedded into the binary.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist returns the built frontend rooted at dist/.
func Dist() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}
