//go:build atomicfallback

package repo

import (
	"io"
	"os"
	"path/filepath"
)

// atomicFallbackWrites is true when the binary is built with the atomicfallback tag (make ATOMIC_FALLBACK=true).
// The fallback file is then staged in a temporary file and renamed over the target, so readers never observe a
// partially written file.
const atomicFallbackWrites = true

// writeFallbackContent stages data in a temporary file and atomically replaces path with it.
func writeFallbackContent(path string, sink func(writer io.Writer) error) error {
	tmpFile, err := os.CreateTemp("", "fallback-*.json")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	err = sink(tmpFile)
	closeErr := tmpFile.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	err = os.Chmod(tmpPath, 0644)
	if err != nil {
		return err
	}

	return replaceFile(tmpPath, path)
}

func replaceFile(src string, dst string) error {
	if os.Rename(src, dst) == nil {
		return nil
	}

	// Rename fails across filesystems (e.g. when /tmp is a separate mount), so stage a copy next to dst
	// and rename that instead, keeping the final replacement atomic
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}

	staged, err := os.CreateTemp(filepath.Dir(dst), filepath.Base(dst)+".*.tmp")
	if err != nil {
		return err
	}
	stagedPath := staged.Name()
	defer func() {
		_ = os.Remove(stagedPath)
	}()

	_, err = staged.Write(data)
	closeErr := staged.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	err = os.Chmod(stagedPath, 0644)
	if err != nil {
		return err
	}

	return os.Rename(stagedPath, dst)
}
