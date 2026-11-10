// Package webui embeds the standalone desktop application frontend assets
// directly into the Go binary using //go:embed so the executable runs on closed
// networks without external web files or Node.js dependencies.
package webui

import (
	_ "embed"
	"net/http"
)

//go:embed index.html
var IndexHTML []byte

//go:embed awx_host_checker.js
var CheckerJS []byte

// ServeIndex serves the standalone app index.html page.
func ServeIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(IndexHTML)
}

// ServeCheckerJS serves the embedded awx_host_checker.js bundle.
func ServeCheckerJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(CheckerJS)
}

