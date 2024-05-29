//go:build noembed

package tmpl

import "os"

func initialize() {
	templates = os.DirFS("./web")
}
