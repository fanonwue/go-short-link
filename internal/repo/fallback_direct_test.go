//go:build !atomicfallback

package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFallbackFileWritesInPlace(t *testing.T) {
	tmp := useTempDirForTmp(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "fallback.json")
	if err := writeFallbackFile(path, testMapping(3)); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := writeFallbackFile(path, testMapping(4)); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("expected direct write to modify the existing file in place")
	}
	// No staging file should be created when writing directly
	assertOnly(t, tmp)
	assertOnly(t, dir, "fallback.json")
}
