//go:build !minify

package minify

import "io"

const EnableMinification = false

func processInternal(r io.Reader) io.Reader {
	return r
}
