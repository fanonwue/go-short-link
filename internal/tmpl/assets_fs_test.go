package tmpl

import (
	"io/fs"
	"testing"
)

// interface assertions
var _ fs.FS = &EmbedLocalFS{}
var _ fs.ReadFileFS = &EmbedLocalFS{}

func TestEmbeddedFilesAreSeekable(t *testing.T) {
	_, err := openEmbeddedFile(embeddedAssets, "secret.txt")
	if err != nil {
		t.Fatal(err)
	}
}
