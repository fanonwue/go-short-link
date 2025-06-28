package minify

import "io"

func FromReader(src io.Reader) io.Reader {
	return processInternal(src)
}
