// Package webui embeds the portal's html/template pages.
package webui

import "embed"

//go:embed templates/*.html
var FS embed.FS
