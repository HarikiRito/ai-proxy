// Package web embeds the built SPA (web/build) so the api binary serves it.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:build
var build embed.FS

// Build is the SPA output rooted at build/.
func Build() (fs.FS, error) {
	return fs.Sub(build, "build")
}
