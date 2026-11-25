package store

import (
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
)

// The store's seams for its own tests alone: the handle DiskPorts made,
// for the subtests that write a log's raw bytes, and a gate around it;
// lit's repository behind the ports; where both live on disk, for the
// tests that ask git and the filesystem; and the batches its tests append
// and the records they read back, one helper for the tests inside the
// package and out.

// FilesOf is the files handle p's store runs over.
func FilesOf(p Ports) files.Files { return p.files }

// WithFiles is p with its logs in f.
func WithFiles(p Ports, f files.Files) Ports {
	p.files = f
	return p
}

// RepositoryDir is lit's repository of at's clone, as DiskPorts opens it.
func RepositoryDir(at git.Checkout) string { return locate(at).repo.Path() }

// StoreDir is the directory of at's clone's store, which lit's repository
// is in, as DiskPorts opens it.
func StoreDir(at git.Checkout) files.Dir { return locate(at).repo.Parent() }

// RepositoryOf is lit's repository p's store runs over.
func RepositoryOf(p Ports) (git.Objects, git.Refs) { return p.objects, p.refs }

// BatchOf is one batch of the records rs, failing t when they make none.
func BatchOf(t testing.TB, rs ...string) Batch {
	t.Helper()
	records := make([]Record, len(rs))
	for i, r := range rs {
		records[i] = Record(r)
	}
	b, err := NewBatch(records...)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// RecordsOf is every record of a scan's frames, in order, as text.
func RecordsOf(s Scan) []string {
	var rs []string
	for _, f := range s.Frames {
		for _, r := range f.Batch.Records() {
			rs = append(rs, string(r))
		}
	}
	return rs
}
