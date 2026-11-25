package gittest

import (
	"errors"
	"testing"

	"github.com/promptctl/lit2/internal/git"
)

// commits is a code repository a test can commit to: Commit makes files,
// path to bytes, the whole tree of a new commit on HEAD.
type commits interface {
	git.Code
	Commit(t *testing.T, files map[string][]byte)
}

// Code holds the code repository on disk and its twin to the Code
// contract.
func Code(t *testing.T) {
	t.Run("repository", func(t *testing.T) {
		code(t, func(t *testing.T) commits { return newRepo(t) })
	})
	t.Run("memory", func(t *testing.T) {
		code(t, func(*testing.T) commits { return memoryCode{&git.CodeMemory{}} })
	})
}

// code holds fresh, a code repository with no commit each call, to the Code
// contract.
func code(t *testing.T, fresh func(*testing.T) commits) {
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
	t.Run("an id of another format is refused, never answered no", func(t *testing.T) {
		c := fresh(t)
		c.Commit(t, map[string][]byte{path: one})
		foreign := git.HashBlob(git.SHA256, one)
		if got, err := c.EverAt(path, foreign); !errors.As(err, new(git.ForeignFormat)) {
			t.Errorf("EverAt(%s) = %v, %v; want ForeignFormat", foreign, got, err)
		}
	})
	t.Run("a blob at another path is not at this one", func(t *testing.T) {
		c := fresh(t)
		c.Commit(t, map[string][]byte{"elsewhere.toml": one})
		ever(t, c, idOne, false)
	})
}

// memoryCode is the code repository's twin, committed to directly.
type memoryCode struct{ *git.CodeMemory }

// Commit commits files.
func (m memoryCode) Commit(_ *testing.T, files map[string][]byte) { m.CodeMemory.Commit(files) }
