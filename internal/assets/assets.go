// Package assets is every file lit2 ships: the default workspace profile and
// BIP-39's word list. Each file is embedded here and named by a declared
// Asset, so a consumer names a value and never a path string
// `[LAW:one-source-of-truth]` `[LAW:types-are-the-program]`.
//
// It imports nothing in the project: it is the bottom of the layers block, so
// config and events both read shipped files through it.
package assets

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed workspace bip39
var files embed.FS

// Asset names one shipped file by its path under this package. Only this
// package mints one, so every Asset resolves to an embedded file.
type Asset struct{ path string }

// The shipped files. Adding one is a file and one value here, and the test
// that holds declared and embedded equal fails until both exist.
var (
	// DefaultWorkspace is the default profile: the workspace model in force
	// until a person commits a version (architecture.md §3.15).
	DefaultWorkspace = Asset{"workspace/default.toml"}
	// BIP39English is BIP-39's English word list, one word per line.
	BIP39English = Asset{"bip39/english.txt"}
)

// declared is every Asset above, for the test that holds them equal to the
// embedded files.
var declared = []Asset{DefaultWorkspace, BIP39English}

// Bytes is the file's content as the binary was built. A missing file is a
// build-time failure, since the test that pairs declared with embedded fails
// first; reaching the panic at run time is a bug in this package.
func (a Asset) Bytes() []byte {
	b, err := fs.ReadFile(files, a.path)
	if err != nil {
		panic(fmt.Sprintf("assets: %s is not embedded: %v", a.path, err))
	}
	return b
}
