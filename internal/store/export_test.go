package store

import (
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
)

// The store's seams for its own tests alone: the handle DiskPorts made,
// for the subtests that write a log's raw bytes, and a gate around it;
// lit's repository behind the ports; and where both live on disk, for the
// tests that ask git and the filesystem.

// FilesOf is the files handle p's store runs over.
func FilesOf(p Ports) files.Files { return p.files }

// WithFiles is p with its logs in f.
func WithFiles(p Ports, f files.Files) Ports {
	p.files = f
	return p
}

// LogDir is the directory of the logs of at's clone, as DiskPorts opens it.
func LogDir(at git.Checkout) files.Dir { return locate(at).logs }

// RepositoryDir is lit's repository of at's clone, as DiskPorts opens it.
func RepositoryDir(at git.Checkout) string { return locate(at).store.PathOf(repoDir) }

// StoreDir is the directory of at's clone's store, lit's repository's
// directory, as DiskPorts opens it.
func StoreDir(at git.Checkout) files.Dir { return locate(at).store }

// RepositoryOf is lit's repository p's store runs over.
func RepositoryOf(p Ports) (git.Objects, git.Refs) { return p.objects, p.refs }
