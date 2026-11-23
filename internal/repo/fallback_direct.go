//go:build !atomicfallback

package repo

import (
	"io"
	"os"

	"github.com/fanonwue/go-short-link/internal/util"
)

// atomicFallbackWrites is false by default. The fallback file is written in place, which is cheaper, but readers
// may observe a partially written file if they read while a write is in progress.
const atomicFallbackWrites = false

// useMemoryBuffer set to true will write marshal the data to memory buffer before writing to file.
const useMemoryBuffer = false

// writeFallbackContent writes data directly to path, truncating any existing file.
func writeFallbackContent(path string, sink func(writer io.Writer) error) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer func() {
		util.FileCloseErrorHandler(file.Close())
	}()

	//noinspection GoBoolExpressions
	if !useMemoryBuffer {
		return sink(file)
	}

	buf := util.NewBuffer(4096)
	err = sink(buf)
	if err != nil {
		return err
	}

	_, err = buf.WriteTo(file)
	return err
}
