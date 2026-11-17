package git

import (
	"errors"
	"fmt"
	"sync"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/filesystem/dotgit"
)

// Code is the code repository as lit2 reads it, never writes it: the blob
// HEAD holds at a path, and whether a blob was ever at a path in HEAD's
// history. A path is slash-separated, from the work tree's root.
type Code interface {
	AtHead(path string) (HeadBlob, error)
	EverAt(path string, id ObjectID) (bool, error)
}

// HeadBlob is what HEAD's tree holds at a path: a blob, or none.
//
//sumtype:decl
type HeadBlob interface{ headBlob() }

// Tracked is a blob HEAD's tree holds at the path.
type Tracked struct{ Blob ObjectID }

// Untracked is no blob at the path in HEAD's tree, or a HEAD with no commit.
type Untracked struct{}

func (Tracked) headBlob()   {}
func (Untracked) headBlob() {}

// CodeAt is the code repository of the checkout c, in format f, read in
// process through go-git's storage: HEAD from the checkout's own git dir,
// so a linked worktree reads its own branch, and refs and objects from the
// common dir. No go-git repository is opened, so no transport is linked.
func CodeAt(c Checkout, f ObjectFormat) Code { return codeRepo{c: c, f: f} }

type codeRepo struct {
	c Checkout
	f ObjectFormat
}

// head is HEAD's commit, or none when HEAD names a branch with no
// commit. go-git v5 reads SHA-1 objects only, so a SHA-256 repository is
// refused, naming why.
func (r codeRepo) head() (*object.Commit, error) {
	if r.f != SHA1 {
		return nil, fmt.Errorf("the code repository at %s is %s, and this build reads only sha1 code repositories", r.c.WorkTree, r.f)
	}
	s := filesystem.NewStorage(dotgit.NewRepositoryFilesystem(osfs.New(r.c.GitDir), osfs.New(r.c.CommonDir)), cache.NewObjectLRUDefault())
	ref, err := storer.ResolveReference(s, plumbing.HEAD)
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading HEAD: %w", err)
	}
	c, err := object.GetCommit(s, ref.Hash())
	if err != nil {
		return nil, fmt.Errorf("reading HEAD's commit %s: %w", ref.Hash(), err)
	}
	return c, nil
}

// blobAt is the blob c's tree holds at path, if a regular file is there.
func blobAt(c *object.Commit, path string) (HeadBlob, error) {
	tree, err := c.Tree()
	if err != nil {
		return nil, fmt.Errorf("reading commit %s's tree: %w", c.Hash, err)
	}
	e, err := tree.FindEntry(path)
	if errors.Is(err, object.ErrEntryNotFound) || errors.Is(err, object.ErrDirectoryNotFound) {
		return Untracked{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding %s in commit %s: %w", path, c.Hash, err)
	}
	if !e.Mode.IsFile() {
		return Untracked{}, nil
	}
	return Tracked{Blob: sha1ID(e.Hash)}, nil
}

func (r codeRepo) AtHead(path string) (HeadBlob, error) {
	c, err := r.head()
	if err != nil || c == nil {
		return Untracked{}, err
	}
	return blobAt(c, path)
}

func (r codeRepo) EverAt(path string, id ObjectID) (bool, error) {
	c, err := r.head()
	if err != nil || c == nil {
		return false, err
	}
	found := false
	err = object.NewCommitPreorderIter(c, nil, nil).ForEach(func(c *object.Commit) error {
		at, err := blobAt(c, path)
		if err != nil {
			return err
		}
		if at == (Tracked{Blob: id}) {
			found = true
			return storer.ErrStop
		}
		return nil
	})
	return found, err
}

// CodeMemory is the code repository in process, SHA-1, its history a list
// of commits, each the whole tree as path to blob. It is Code's twin, held
// to the same contract suite.
type CodeMemory struct {
	mu      sync.Mutex
	commits []map[string]ObjectID
}

// Commit makes files, path to bytes, the tree of a new commit on HEAD; a
// path not in files is not in the tree.
func (m *CodeMemory) Commit(files map[string][]byte) {
	tree := make(map[string]ObjectID, len(files))
	for p, b := range files {
		tree[p] = HashBlob(SHA1, b)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commits = append(m.commits, tree)
}

func (m *CodeMemory) AtHead(path string) (HeadBlob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.commits) == 0 {
		return Untracked{}, nil
	}
	if id, ok := m.commits[len(m.commits)-1][path]; ok {
		return Tracked{Blob: id}, nil
	}
	return Untracked{}, nil
}

func (m *CodeMemory) EverAt(path string, id ObjectID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.commits {
		if at, ok := c[path]; ok && at == id {
			return true, nil
		}
	}
	return false, nil
}
