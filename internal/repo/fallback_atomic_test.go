//go:build atomicfallback

package repo

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestReplaceFileReplacesDestinationContent(t *testing.T) {
	tmp := useTempDirForTmp(t)
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst.json")
	src := filepath.Join(tmp, "src.json")

	if err := os.WriteFile(dst, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("new content"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := replaceFile(src, dst); err != nil {
		t.Fatalf("replaceFile returned error: %v", err)
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new content" {
		t.Fatalf("destination content: got %q, want %q", data, "new content")
	}
	assertOnly(t, dir, "dst.json")
}

func TestReplaceFileMissingSourceKeepsDestination(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst.json")
	if err := os.WriteFile(dst, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := replaceFile(filepath.Join(dir, "missing-src"), dst); err == nil {
		t.Fatal("expected error when source does not exist")
	}

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "original" {
		t.Fatalf("destination was modified on failure: %q", data)
	}
	assertOnly(t, dir, "dst.json")
}

// TestReplaceFileCrossDeviceFallback exercises the copy-and-rename branch of replaceFile, which is taken when
// os.Rename fails because source and destination are on different filesystems. The test only runs when the
// rename really fails here, so it needs /tmp and /dev/shm (or another path) on different mounts.
func TestReplaceFileCrossDeviceFallback(t *testing.T) {
	tmp := useTempDirForTmp(t)
	shm, err := os.MkdirTemp("/dev/shm", "fallback-xdev-*")
	if err != nil {
		t.Skipf("no second filesystem available: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(shm) })

	src := filepath.Join(tmp, "src.json")
	if err := os.WriteFile(src, []byte("cross device"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(shm, "dst.json")
	if err := os.WriteFile(dst, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}

	// Probe that a rename really crosses devices, otherwise this test would not cover the fallback branch
	probe := filepath.Join(shm, "probe")
	if err := os.Rename(src, probe); err == nil {
		t.Skip("rename across these directories succeeds, so the cross-device branch is not reachable here")
	}

	if err := replaceFile(src, dst); err != nil {
		t.Fatalf("replaceFile returned error: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "cross device" {
		t.Fatalf("destination content: got %q, want %q", data, "cross device")
	}
	// Staged copy must be gone, and the source is only removed by the caller
	assertOnly(t, shm, "dst.json")
}

// TestReplaceFileFailureCleansUpStagedCopy forces the copy branch to fail at the final rename (destination is a
// non-empty directory) and checks that the staged file is removed and the directory is left alone.
func TestReplaceFileFailureCleansUpStagedCopy(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "dst")
	if err := os.Mkdir(dst, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "child"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(t.TempDir(), "src.json")
	if err := os.WriteFile(src, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := replaceFile(src, dst); err == nil {
		t.Fatal("expected error when destination is a non-empty directory")
	}
	assertOnly(t, dir, "dst")
	assertOnly(t, dst, "child")
}

// TestWriteFallbackFileIsAtomicForReaders writes alternating large mappings while readers continuously load the
// file. Because the file is replaced via rename, every read must observe a complete previous or new version and
// never a partially written one.
func TestWriteFallbackFileIsAtomicForReaders(t *testing.T) {
	useTempDirForTmp(t)
	path := filepath.Join(t.TempDir(), "fallback.json")

	small := testMapping(10)
	large := testMapping(5000)
	if err := writeFallbackFile(path, small); err != nil {
		t.Fatal(err)
	}

	const writes = 200
	var (
		wg       sync.WaitGroup
		stop     atomic.Bool
		readErrs = make(chan error, 1)
	)

	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				entries, err := readFallbackFile(path)
				if err != nil {
					select {
					case readErrs <- err:
					default:
					}
					return
				}
				if n := len(entries); n != len(small) && n != len(large) {
					select {
					case readErrs <- os.ErrInvalid:
					default:
					}
					return
				}
			}
		}()
	}

	for i := range writes {
		m := small
		if i%2 == 1 {
			m = large
		}
		if err := writeFallbackFile(path, m); err != nil {
			t.Fatalf("write %d failed: %v", i, err)
		}
	}
	stop.Store(true)
	wg.Wait()

	select {
	case err := <-readErrs:
		t.Fatalf("reader observed an inconsistent fallback file: %v", err)
	default:
	}
}

func TestWriteFallbackFileReplacesFileAtomically(t *testing.T) {
	useTempDirForTmp(t)
	path := filepath.Join(t.TempDir(), "fallback.json")
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
	// A rename replaces the directory entry with a new file, so the inode must change
	if os.SameFile(before, after) {
		t.Fatal("expected atomic write to replace the file, but it was modified in place")
	}
}
