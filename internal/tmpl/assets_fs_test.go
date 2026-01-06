package tmpl

import "io/fs"

// interface assertions
var _ fs.FS = &EmbedLocalFS{}
var _ fs.ReadFileFS = &EmbedLocalFS{}
