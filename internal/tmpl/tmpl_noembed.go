//go:build noembed

package tmpl

import "os"

func initialize() {
	if initialized {
		return
	}
	templates = os.DirFS("./web")
	initialized = true
}
