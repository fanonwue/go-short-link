package repo

import (
	"encoding/json/v2"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/fanonwue/go-short-link/internal/state"
)

func testMapping(n int) state.RedirectMap {
	m := make(state.RedirectMap, n)
	for i := range n {
		m[filepath.Join("key", string(rune('a'+i%26)), strconv.Itoa(i))] = "https://example.com/" + strconv.Itoa(i)
	}
	return m
}

// useTempDirForTmp points os.TempDir (and therefore os.CreateTemp("", ...)) at a fresh directory so tests can
// check that no temporary files are left behind.
func useTempDirForTmp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	return dir
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading dir %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func assertOnly(t *testing.T, dir string, want ...string) {
	t.Helper()
	got := listDir(t, dir)
	if len(got) != len(want) {
		t.Fatalf("dir %s: got entries %v, want %v", dir, got, want)
	}
	seen := make(map[string]bool, len(got))
	for _, g := range got {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			t.Fatalf("dir %s: got entries %v, want %v", dir, got, want)
		}
	}
}

func TestWriteAndReadFallbackFileRoundTrip(t *testing.T) {
	useTempDirForTmp(t)
	path := filepath.Join(t.TempDir(), "fallback.json")
	want := state.RedirectMap{
		"github": "https://github.com",
		"docs":   "https://example.com/docs",
	}

	if err := writeFallbackFile(path, want); err != nil {
		t.Fatalf("writeFallbackFile returned error: %v", err)
	}

	got, err := readFallbackMap(path)
	if err != nil {
		t.Fatalf("readFallbackMap returned error: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %v", len(got), len(want), got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("key %q: got %q, want %q", k, got[k], v)
		}
	}
}

func TestWriteFallbackFileEmptyMappingWritesEmptyArray(t *testing.T) {
	useTempDirForTmp(t)
	path := filepath.Join(t.TempDir(), "empty.json")

	if err := writeFallbackFile(path, state.RedirectMap{}); err != nil {
		t.Fatalf("writeFallbackFile returned error: %v", err)
	}

	got, err := readFallbackMap(path)
	if err != nil {
		t.Fatalf("readFallbackMap returned error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty mapping, got %v", got)
	}
}

func TestWriteFallbackFileEmptyPathIsNoop(t *testing.T) {
	tmp := useTempDirForTmp(t)

	if err := writeFallbackFile("", testMapping(3)); err != nil {
		t.Fatalf("expected no error for empty path, got %v", err)
	}
	assertOnly(t, tmp)
}

func TestWriteFallbackFileCreatesParentDirectories(t *testing.T) {
	useTempDirForTmp(t)
	path := filepath.Join(t.TempDir(), "nested", "deeper", "fallback.json")

	if err := writeFallbackFile(path, testMapping(2)); err != nil {
		t.Fatalf("writeFallbackFile returned error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected fallback file at %s: %v", path, err)
	}
}

func TestWriteFallbackFileParentIsRegularFileFails(t *testing.T) {
	useTempDirForTmp(t)
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeFallbackFile(filepath.Join(blocker, "fallback.json"), testMapping(1)); err == nil {
		t.Fatal("expected error when parent path is a regular file")
	}
}

func TestWriteFallbackFileSetsReadablePermissions(t *testing.T) {
	useTempDirForTmp(t)
	path := filepath.Join(t.TempDir(), "fallback.json")

	if err := writeFallbackFile(path, testMapping(1)); err != nil {
		t.Fatalf("writeFallbackFile returned error: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0644 {
		t.Fatalf("got permissions %o, want 0644", perm)
	}
}

func TestWriteFallbackFileOverwritesExistingFile(t *testing.T) {
	tmp := useTempDirForTmp(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "fallback.json")

	if err := writeFallbackFile(path, state.RedirectMap{"old": "https://old.example"}); err != nil {
		t.Fatal(err)
	}
	if err := writeFallbackFile(path, state.RedirectMap{"new": "https://new.example"}); err != nil {
		t.Fatal(err)
	}

	got, err := readFallbackMap(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got["old"]; ok {
		t.Fatalf("old entry still present after overwrite: %v", got)
	}
	if got["new"] != "https://new.example" {
		t.Fatalf("new entry missing after overwrite: %v", got)
	}
	// Only the destination should remain, no staged or temporary files
	assertOnly(t, dir, "fallback.json")
	assertOnly(t, tmp)
}

func TestWriteFallbackFileLeavesNoTempFilesBehind(t *testing.T) {
	tmp := useTempDirForTmp(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "fallback.json")

	if err := writeFallbackFile(path, testMapping(50)); err != nil {
		t.Fatal(err)
	}
	assertOnly(t, tmp)
	assertOnly(t, dir, "fallback.json")
}

func TestWriteFallbackFileLogReturnsError(t *testing.T) {
	useTempDirForTmp(t)
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeFallbackFileLog(filepath.Join(blocker, "fallback.json"), testMapping(1)); err == nil {
		t.Fatal("expected writeFallbackFileLog to return the underlying error")
	}
}

func TestReadFallbackFileMissingReturnsError(t *testing.T) {
	if _, err := readFallbackMap(filepath.Join(t.TempDir(), "does-not-exist.json")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadFallbackMapLogMissingReturnsError(t *testing.T) {
	if _, err := readFallbackMapLog(filepath.Join(t.TempDir(), "does-not-exist.json")); err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadFallbackFileInvalidJSONReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.json")
	if err := os.WriteFile(path, []byte(`[{"key": "a", "target": `), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readFallbackMap(path); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestReadFallbackFileWrongShapeReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "object.json")
	if err := os.WriteFile(path, []byte(`{"key": "a"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readFallbackMap(path); err == nil {
		t.Fatal("expected error when JSON is not an array of entries")
	}
}

func TestReadFallbackFileReadsExternalFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "external.json")
	content := `[{"key":"a","target":"https://a.example"},{"key":"b","target":"https://b.example"}]`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	entries, err := readFallbackFile(path)
	if err != nil {
		t.Fatalf("readFallbackFile returned error: %v", err)
	}
	if len(entries) != 2 || entries[0] != (FallbackFileEntry{Key: "a", Target: "https://a.example"}) {
		t.Fatalf("unexpected entries: %+v", entries)
	}
}

func TestFallbackFileEntryJSONTags(t *testing.T) {
	data, err := json.Marshal(FallbackFileEntry{Key: "k", Target: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"key":"k","target":"t"}` {
		t.Fatalf("unexpected JSON encoding: %s", data)
	}
}

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
