// Package web embeds the built PWA static assets so the backend can serve
// them from the same process and binary as the API.
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var files embed.FS

// StaticFS returns the embedded filesystem rooted at static/, so paths omit
// the "static/" prefix (e.g. "index.html", "manifest.webmanifest").
func StaticFS() (fs.FS, error) {
	return fs.Sub(files, "static")
}
