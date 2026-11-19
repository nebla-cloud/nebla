// Package web embeds the built SPA.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Dist is the SPA root.
var Dist, _ = fs.Sub(distFS, "dist")
