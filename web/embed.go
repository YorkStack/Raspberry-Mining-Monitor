// Package web embeds the dashboard frontend into the binary.
//
// Everything the page needs ships here, so the deployed artefact is one file
// and the page never reaches outside its own origin.
package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var dist embed.FS

// Assets returns the frontend rooted at the directory holding index.html.
func Assets() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic("web: embedded assets missing: " + err.Error())
	}
	return sub
}
