package tmpl

import (
	"embed"
	"errors"
	"io"
	"io/fs"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fanonwue/goutils/logging"
)

const AssetsPathPrefix = "assets/"
const AssetsPathLocalFS = "./data/" + AssetsPathPrefix

var ErrEmbeddedFileNotSeekable = errors.New("embedded file is not seekable")

type SeekableFile interface {
	fs.File
	io.ReadSeeker
}

type AssetFile struct {
	SeekableFile
	Source AssetFileSource
}

func (af AssetFile) IsImmutable() bool {
	return af.Source.IsImmutable()
}

// ModTimeOrDefault returns the modification time of the asset file.
// If the asset file does not have a modification time and is considered immutable (like an embedded file),
// it returns the immutableModTime parameter.
func (af AssetFile) ModTimeOrDefault(immutableModTime time.Time) (time.Time, error) {
	stat, err := af.Stat()
	if err != nil {
		return time.Time{}, err
	}
	modTime := stat.ModTime()
	if modTime.IsZero() && af.Source.IsImmutable() {
		modTime = immutableModTime
	}
	return modTime, nil
}

type AssetFileSource uint8

func (afs AssetFileSource) String() string {
	switch afs {
	case AssetFileSourceEmbedded:
		return "embedded"
	case AssetFileSourceLocal:
		return "local"
	}
	panic("unknown asset file source")
}

func (afs AssetFileSource) IsImmutable() bool {
	if afs == AssetFileSourceEmbedded {
		return true
	}
	return false
}

const (
	AssetFileSourceEmbedded AssetFileSource = iota + 1
	AssetFileSourceLocal
)

type EmbedLocalFS struct {
	embedded embed.FS
	local    *os.Root
}

func (elfs *EmbedLocalFS) normalizeName(name string) string {
	if name[0] == '/' {
		name = name[1:]
	}
	return name
}

func (elfs *EmbedLocalFS) ReadFile(name string) ([]byte, error) {
	name = elfs.normalizeName(name)
	if elfs.local != nil {
		// Try to open the file from the local file system first
		r, err := elfs.local.ReadFile(name)
		if err == nil {
			return r, nil
		}
	}
	// Try to open the file from the embedded assets if the local file system does not have such a file
	return elfs.embedded.ReadFile(AssetsPathPrefix + name)
}

func (elfs *EmbedLocalFS) Open(name string) (fs.File, error) {
	af, err := elfs.OpenAssetFile(name)
	if err != nil {
		return nil, err
	}
	return af.SeekableFile, err
}

func (elfs *EmbedLocalFS) OpenAssetFile(name string) (AssetFile, error) {
	name = elfs.normalizeName(name)
	if elfs.local != nil {
		// Try to open the file from the local file system first
		file, err := elfs.local.Open(name)
		if err == nil {
			return AssetFile{
				Source:       AssetFileSourceLocal,
				SeekableFile: file,
			}, nil
		}
	}
	// Try to open the file from the embedded assets if the local file system does not have such a file
	return openEmbeddedFile(elfs.embedded, name)
}

func (elfs *EmbedLocalFS) FS() fs.FS { return elfs }

func (elfs *EmbedLocalFS) EmbeddedFS() embed.FS { return elfs.embedded }
func (elfs *EmbedLocalFS) LocalFS() *os.Root    { return elfs.local }
func (elfs *EmbedLocalFS) LocalPath() string {
	if elfs.local == nil {
		return ""
	}
	return elfs.local.Name()
}

// Close releases the resources held by the local file system, if any.
// The embedded assets need no cleanup. Close is safe to call on an instance without a local FS.
func (elfs *EmbedLocalFS) Close() error {
	if elfs.local == nil {
		return nil
	}
	return elfs.local.Close()
}

func openEmbeddedFile(fsys fs.FS, name string) (AssetFile, error) {
	file, err := fsys.Open(AssetsPathPrefix + name)
	if err != nil {
		return AssetFile{}, err
	}
	seekableFile, ok := file.(SeekableFile)
	if !ok {
		return AssetFile{}, ErrEmbeddedFileNotSeekable
	}
	return AssetFile{
		Source:       AssetFileSourceEmbedded,
		SeekableFile: seekableFile,
	}, err
}

var (
	//go:embed assets
	embeddedAssets embed.FS
	assets         atomic.Pointer[EmbedLocalFS]
	assetsMut      sync.Mutex
)

func createAssetsFS() *EmbedLocalFS {
	localFs, err := os.OpenRoot(AssetsPathLocalFS)
	if err != nil {
		logging.Warnf("Could not open local file system, ignoring local FS: %v", err)
	}
	return &EmbedLocalFS{
		embedded: embeddedAssets,
		local:    localFs,
	}
}

// AssetsFS returns the shared instance of the active EmbedLocalFS, initializing it on first use.
func AssetsFS() *EmbedLocalFS {
	if a := assets.Load(); a != nil {
		return a
	}
	assetsMut.Lock()
	defer assetsMut.Unlock()
	// Re-check condition under lock to check whether it was created while we were waiting for the lock
	if a := assets.Load(); a != nil {
		return a
	}
	a := createAssetsFS()
	assets.Store(a)
	return a
}

// CloseAssetsFS closes the shared instance if it was ever created.
func CloseAssetsFS() error {
	if a := assets.Load(); a != nil {
		logging.Infof("Closing assets FS")
		return a.Close()
	}
	return nil
}
