package tmpl

import (
	"embed"
	"io/fs"
	"os"
	"sync"

	"github.com/fanonwue/goutils/logging"
)

const AssetsPathPrefix = "assets/"
const AssetsPathLocalFS = "./data/" + AssetsPathPrefix

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
	name = elfs.normalizeName(name)
	if elfs.local != nil {
		// Try to open the file from the local file system first
		file, err := elfs.local.Open(name)
		if err == nil {
			return file, nil
		}
	}
	// Try to open the file from the embedded assets if the local file system does not have such a file
	return elfs.embedded.Open(AssetsPathPrefix + name)
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

//go:embed assets
var embeddedAssets embed.FS
var assets *EmbedLocalFS

// assets cannot be initialized more than once, so there is no need for a RWMutex
var assetsMut = &sync.Mutex{}

func CreateAssetsFS() *EmbedLocalFS {
	localFs, err := os.OpenRoot(AssetsPathLocalFS)
	if err != nil {
		logging.Warnf("Could not open local file system, ignoring local FS: %v", err)
	}
	return &EmbedLocalFS{
		embedded: embeddedAssets,
		local:    localFs,
	}
}

// initAssetsFs will create an instance of AssetsFS. Only one instance can be active.
// Use of a mutex ensures synchronization.
func initAssetsFs() {
	assetsMut.Lock()
	defer assetsMut.Unlock()
	// Check again under lock
	if assets != nil {
		return
	}
	assets = CreateAssetsFS()
}

// AssetsFS returns the shared instance of the active EmbedLocalFS, initializing it when needed.
// Initialization is synchronized.
func AssetsFS() *EmbedLocalFS {
	if assets == nil {
		initAssetsFs()
	}
	return assets
}
