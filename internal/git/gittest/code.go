package gittest

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/promptctl/lit2/internal/git"
)

// Commits is a code repository a test can commit to: Commit makes files,
// path to bytes, the whole tree of a new commit on HEAD.
type Commits interface {
	git.Code
	Commit(t *testing.T, files map[string][]byte)
}

// Code holds fresh, a code repository with no commit each call, to the Code
// contract.
func Code(t *testing.T, fresh func(*testing.T) Commits) {
	const path = ".lit/workspace.toml"
	one, two := []byte("prefix = \"a\"\n"), []byte("prefix = \"b\"\n")
	idOne, idTwo := git.HashBlob(git.SHA1, one), git.HashBlob(git.SHA1, two)
	at := func(t *testing.T, c git.Code, want git.HeadBlob) {
		t.Helper()
		got, err := c.AtHead(path)
		if err != nil || got != want {
			t.Errorf("AtHead = %#v, %v; want %#v", got, err, want)
		}
	}
	ever := func(t *testing.T, c git.Code, id git.ObjectID, want bool) {
		t.Helper()
		got, err := c.EverAt(path, id)
		if err != nil || got != want {
			t.Errorf("EverAt(%s) = %v, %v; want %v", id, got, err, want)
		}
	}
	t.Run("a HEAD with no commit holds nothing", func(t *testing.T) {
		c := fresh(t)
		at(t, c, git.Untracked{})
		ever(t, c, idOne, false)
	})
	t.Run("a path no commit holds is untracked", func(t *testing.T) {
		c := fresh(t)
		c.Commit(t, map[string][]byte{"README": one})
		at(t, c, git.Untracked{})
		ever(t, c, idOne, false)
	})
	t.Run("a committed file is HEAD's blob, git's id for its bytes", func(t *testing.T) {
		c := fresh(t)
		c.Commit(t, map[string][]byte{path: one})
		at(t, c, git.Tracked{Blob: idOne})
		ever(t, c, idOne, true)
		ever(t, c, idTwo, false)
	})
	t.Run("a recommitted file keeps its old blob in the history", func(t *testing.T) {
		c := fresh(t)
		c.Commit(t, map[string][]byte{path: one})
		c.Commit(t, map[string][]byte{path: two})
		at(t, c, git.Tracked{Blob: idTwo})
		ever(t, c, idOne, true)
		c.Commit(t, map[string][]byte{"README": one})
		at(t, c, git.Untracked{})
		ever(t, c, idTwo, true)
	})
	t.Run("a directory on the path that was a file holds nothing", func(t *testing.T) {
		c := fresh(t)
		c.Commit(t, map[string][]byte{".lit": one})
		c.Commit(t, map[string][]byte{path: two})
		ever(t, c, idTwo, true)
		ever(t, c, idOne, false)
	})
	t.Run("a blob at another path is not at this one", func(t *testing.T) {
		c := fresh(t)
		c.Commit(t, map[string][]byte{"elsewhere.toml": one})
		ever(t, c, idOne, false)
	})
}

// Repo is a code repository on disk with no commit, committed to through
// the git binary.
type Repo struct {
	git.Code
	Dir string
}

// NewRepo is an empty code repository on disk, read as lit2 reads one.
func NewRepo(t *testing.T) Repo {
	t.Helper()
	dir := Physical(t)
	Git(t, dir, "init", "-q", "-b", "main")
	g := Runner(t)
	c, err := g.Locate(dir)
	if err != nil {
		t.Fatal(err)
	}
	return Repo{Code: g.CodeAt(c, git.SHA1), Dir: dir}
}

// Commit writes files as the work tree's whole content and commits it.
func (r Repo) Commit(t *testing.T, files map[string][]byte) {
	t.Helper()
	Git(t, r.Dir, "rm", "-rqf", "--ignore-unmatch", ".")
	for p, b := range files {
		full := filepath.Join(r.Dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, b, 0o644); err != nil {
			t.Fatal(err)
		}
		Git(t, r.Dir, "add", "--", p)
	}
	Git(t, r.Dir, "commit", "-q", "--allow-empty", "-m", "commit")
}

// Memory is the code repository's twin, committed to directly.
type Memory struct{ *git.CodeMemory }

// Commit commits files.
func (m Memory) Commit(_ *testing.T, files map[string][]byte) { m.CodeMemory.Commit(files) }
