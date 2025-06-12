//go:build noembed

package tmpl

import (
	"log/slog"
	"os"
)

func initialize() {
	webRoot, err := os.OpenRoot("./web")
	if err != nil {
		slog.Error("Failed to read web templates", "err", err)
	}
	templates = webRoot.FS()
}
