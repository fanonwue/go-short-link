//go:build noembed

package tmpl

import (
	"github.com/fanonwue/go-short-link/internal/util"
	"os"
)

func init() {
	webRoot, err := os.OpenRoot("./web")
	if err != nil {
		util.Logger().Fatalf("Failed to read web templates: %v", err)
	}
	templates = webRoot.FS()
}
