// Package web embeds the built frontend (web/dist) into the Go binary.
// Run `npm run build` in this directory (or `make build-web`) before `go build`.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
