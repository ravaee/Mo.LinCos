// Package ui holds the page the agent serves, built into the binary.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed static
var static embed.FS

// Files is the page's folder: index.html, app.js, style.css.
var Files, _ = fs.Sub(static, "static")
