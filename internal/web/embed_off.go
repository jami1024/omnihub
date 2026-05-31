//go:build devui

// Package web (devui variant): no frontend is embedded. Use this build
// tag while iterating on the React app with `vite dev`; the Vite dev
// server proxies /admin/api/* to this Go process on its own port.
package web

import (
	"io/fs"
	"testing/fstest"
)

// AssetsFS returns an empty FS in the devui build. The SPA handler
// gives up and the operator is expected to hit the Vite dev URL
// (e.g. http://localhost:5173) instead.
func AssetsFS() fs.FS { return fstest.MapFS{} }

// Available reports false so main.go can log a "skipping admin UI
// mount, devui build" notice and not register the static handler.
func Available() bool { return false }
