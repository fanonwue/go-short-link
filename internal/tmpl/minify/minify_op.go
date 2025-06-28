//go:build minify

package minify

import (
	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/css"
	"github.com/tdewolff/minify/v2/html"
	"github.com/tdewolff/minify/v2/js"
	"io"
	"regexp"
)

const EnableMinification = true

var m *minify.M

func init() {
	m = minify.New()
	m.AddFunc("text/html", html.Minify)
	m.AddFunc("text/css", css.Minify)
	m.AddFuncRegexp(regexp.MustCompile("^(application|text)/(x-)?(java|ecma)script$"), js.Minify)
}

func processInternal(r io.Reader) io.Reader {
	return m.Reader("text/html", r)
}
