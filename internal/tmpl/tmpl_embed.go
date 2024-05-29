//go:build !noembed

package tmpl

import (
	"embed"
	"io/fs"
)

//go:embed html
var embeddedTemplates embed.FS

func initialize() {
	templates = fs.FS(embeddedTemplates)
}
