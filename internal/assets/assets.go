package assets

import "embed"

//go:embed all:dist
var Frontend embed.FS

// HasFrontend checks if the frontend dist was embedded at build time.
func HasFrontend() bool {
	f, err := Frontend.Open("dist/index.html")
	if err != nil {
		return false
	}
	f.Close()
	return true
}
