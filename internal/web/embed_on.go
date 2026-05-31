//go:build !devui

// Package web embeds the compiled React admin UI into the Go binary
// and exposes a gin handler that serves it under /admin/*filepath with
// an SPA fallback to index.html.
//
// In production (default build tags), `dist/` carries the Vite build
// output. During frontend development build with `-tags devui` to swap
// in an empty FS so the Vite dev server (on its own port) takes over.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var bundle embed.FS

// AssetsFS returns the embedded "dist" subtree as an io/fs.FS.
//
// The empty path component matters: we want the file paths to be
// "index.html" and "assets/index-*.js", not "dist/index.html" etc.,
// so the SPA handler can map URL paths directly.
func AssetsFS() fs.FS {
	sub, err := fs.Sub(bundle, "dist")
	if err != nil {
		// Cannot happen — the embed directive guarantees "dist" exists
		// at build time. Panic so we notice if the directive is ever
		// misedited.
		panic("web: embed missing 'dist': " + err.Error())
	}
	return sub
}

// Available reports whether a real frontend bundle is present.
// Production builds always return true; the devui-tagged variant
// returns false so the wiring code can log and skip mount.
func Available() bool { return true }
