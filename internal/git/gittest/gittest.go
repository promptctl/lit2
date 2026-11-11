// Package gittest is the contract suites of lit's repository's two ports,
// Objects and Refs, which the repository on disk and its twin in memory
// both pass, so a test over the twin is a test over the repository.
package gittest

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/promptctl/lit2/internal/git"
)

// ID is the SHA-1 id s names, for a test that writes one by hand.
func ID(t *testing.T, s string) git.ObjectID {
	t.Helper()
	id, err := git.ParseObjectID(git.SHA1, s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// The ids git gives these objects, as git hash-object prints them.
const (
	helloBlob = "ce013625030ba8dba906f756967f9e9ca394464a" // "hello\n"
	emptyBlob = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
)

// Objects holds fresh, an empty object database each call, to the Objects
// contract.
func Objects(t *testing.T, fresh func(*testing.T) git.Objects) {
	t.Run("a blob's id is git's, and a second put finds it", func(t *testing.T) {
		db := fresh(t)
		for _, c := range []struct {
			content string
			id      string
		}{{"hello\n", helloBlob}, {"", emptyBlob}} {
			id, stored, err := db.PutBlob([]byte(c.content))
			if err != nil || id != ID(t, c.id) || stored != git.New {
				t.Errorf("PutBlob(%q) = %v, %v, %v; want %s, New", c.content, id, stored, err, c.id)
			}
			again, stored, err := db.PutBlob([]byte(c.content))
			if err != nil || again != id || stored != git.Existing {
				t.Errorf("a second PutBlob(%q) = %v, %v, %v; want %s, Existing", c.content, again, stored, err, id)
			}
			b, err := db.Blob(id)
			if err != nil || string(b) != c.content {
				t.Errorf("Blob(%s) = %q, %v; want %q", id, b, err, c.content)
			}
			size, err := db.BlobSize(id)
			if err != nil || size != uint64(len(c.content)) {
				t.Errorf("BlobSize(%s) = %d, %v; want %d", id, size, err, len(c.content))
			}
		}
	})
	t.Run("an id it does not hold is missing", func(t *testing.T) {
		db := fresh(t)
		absent := ID(t, helloBlob)
		_, errBlob := db.Blob(absent)
		_, errSize := db.BlobSize(absent)
		_, errTree := db.Tree(absent)
		_, errCommit := db.Commit(absent)
		for _, err := range []error{errBlob, errSize, errTree, errCommit} {
			if !errors.As(err, new(git.MissingObject)) {
				t.Errorf("reading an absent object failed with %v; want MissingObject", err)
			}
		}
	})
	t.Run("a tree reads back sorted, whatever order it was put in", func(t *testing.T) {
		db := fresh(t)
		empty, _, err := db.PutTree(nil)
		if err != nil || empty != ID(t, emptyTree) {
			t.Errorf("PutTree(nil) = %v, %v; want git's empty tree %s", empty, err, emptyTree)
		}
		hello, _, _ := db.PutBlob([]byte("hello\n"))
		nothing, _, _ := db.PutBlob(nil)
		es := []git.Entry{{Name: "b", Blob: hello}, {Name: "a", Blob: nothing}}
		id, stored, err := db.PutTree(es)
		if err != nil || stored != git.New {
			t.Fatalf("PutTree = %v, %v, %v", id, stored, err)
		}
		again, stored, err := db.PutTree([]git.Entry{es[1], es[0]})
		if err != nil || again != id || stored != git.Existing {
			t.Errorf("the same entries in another order are %v, %v, %v; want %s, Existing", again, stored, err, id)
		}
		got, err := db.Tree(id)
		if err != nil || !slices.Equal(got, []git.Entry{es[1], es[0]}) {
			t.Errorf("Tree(%s) = %v, %v; want %v", id, got, err, []git.Entry{es[1], es[0]})
		}
	})
	t.Run("a tree git would refuse is refused", func(t *testing.T) {
		db := fresh(t)
		blob, _, _ := db.PutBlob([]byte("x"))
		for _, es := range [][]git.Entry{
			{{Name: "a", Blob: blob}, {Name: "a", Blob: blob}},
			{{Name: "a/b", Blob: blob}},
			{{Name: "", Blob: blob}},
			{{Name: "..", Blob: blob}},
		} {
			if _, _, err := db.PutTree(es); err == nil {
				t.Errorf("PutTree(%v) stored a tree", es)
			}
		}
	})
	t.Run("a commit is a pure function of its tree and message", func(t *testing.T) {
		db := fresh(t)
		tree, _, _ := db.PutTree(nil)
		c := git.Commit{Tree: tree, Message: "lit-log v1 x\n"}
		id, stored, err := db.PutCommit(c)
		if err != nil || stored != git.New {
			t.Fatalf("PutCommit = %v, %v, %v", id, stored, err)
		}
		again, stored, err := db.PutCommit(c)
		if err != nil || again != id || stored != git.Existing {
			t.Errorf("the same commit again is %v, %v, %v; want %s, Existing", again, stored, err, id)
		}
		if got, err := db.Commit(id); err != nil || got != c {
			t.Errorf("Commit(%s) = %+v, %v; want %+v", id, got, err, c)
		}
		other, _, _ := db.PutCommit(git.Commit{Tree: tree, Message: "lit-log v1 y\n"})
		if other == id {
			t.Errorf("two messages made one commit %s", id)
		}
	})
	t.Run("an id of another format, or the zero id, names nothing in it", func(t *testing.T) {
		db := fresh(t)
		sha256, err := git.ParseObjectID(git.SHA256, "6c19f8bd0d8a2b5a6c0b2e8e3c3a0f9e2d1c7b6a5f4e3d2c1b0a9f8e7d6c5b4a")
		if err != nil {
			t.Fatal(err)
		}
		for _, foreign := range []git.ObjectID{sha256, {}} {
			_, errBlob := db.Blob(foreign)
			_, errSize := db.BlobSize(foreign)
			_, errTree := db.Tree(foreign)
			_, errCommit := db.Commit(foreign)
			_, _, errPutTree := db.PutTree([]git.Entry{{Name: "a", Blob: foreign}})
			_, _, errPutCommit := db.PutCommit(git.Commit{Tree: foreign})
			for _, err := range []error{errBlob, errSize, errTree, errCommit, errPutTree, errPutCommit} {
				if !errors.As(err, new(git.ForeignFormat)) || err.Error() == "" {
					t.Errorf("the %s id %s was taken with %v; want ForeignFormat", foreign.Format(), foreign, err)
				}
			}
		}
	})
}

// Refs holds fresh, an empty repository each call, to the Refs contract.
func Refs(t *testing.T, fresh func(*testing.T) git.Refs) {
	a, b := ID(t, helloBlob), ID(t, emptyBlob)
	list := func(t *testing.T, refs git.Refs, prefix git.RefName) []git.RefHead {
		t.Helper()
		heads, bad, err := refs.List(prefix)
		if err != nil || len(bad) != 0 {
			t.Fatalf("List(%s) = %v, %v, %v", prefix, heads, bad, err)
		}
		return heads
	}
	t.Run("a fresh repository holds no ref", func(t *testing.T) {
		if heads := list(t, fresh(t), "refs/lit/"); len(heads) != 0 {
			t.Errorf("a fresh repository lists %v", heads)
		}
	})
	t.Run("a replace is listed, and replaced again", func(t *testing.T) {
		refs := fresh(t)
		const name git.RefName = "refs/lit/log/w"
		for _, to := range []git.ObjectID{a, b} {
			if err := refs.Replace(name, to); err != nil {
				t.Fatal(err)
			}
			if heads := list(t, refs, "refs/lit/"); !slices.Equal(heads, []git.RefHead{{Name: name, Target: to}}) {
				t.Errorf("after Replace(%s, %s) the refs are %v", name, to, heads)
			}
		}
	})
	t.Run("a name outside refs/, unclean, or with a component a lock or temporary file takes is refused", func(t *testing.T) {
		refs := fresh(t)
		for _, name := range []git.RefName{"HEAD", "lit/log/w", "refs/lit/../w", "refs/lit//w", "refs/lit/w/", "refs/lit/.w", "refs/lit/.ref-1", "refs/lit/w.lock"} {
			if err := refs.Replace(name, a); err == nil {
				t.Errorf("Replace(%q) = nil; want a refusal", name)
			}
		}
		if heads := list(t, refs, "refs/"); len(heads) != 0 {
			t.Errorf("after refused replaces the refs are %v", heads)
		}
	})
	t.Run("a list is sorted and holds only its prefix", func(t *testing.T) {
		refs := fresh(t)
		for _, name := range []git.RefName{"refs/lit/log/b", "refs/lit/inbox/c", "refs/lit/log/a", "refs/other/d"} {
			if err := refs.Replace(name, a); err != nil {
				t.Fatal(err)
			}
		}
		want := []git.RefHead{{Name: "refs/lit/inbox/c", Target: a}, {Name: "refs/lit/log/a", Target: a}, {Name: "refs/lit/log/b", Target: a}}
		if heads := list(t, refs, "refs/lit/"); !slices.Equal(heads, want) {
			t.Errorf("List(refs/lit/) = %v; want %v", heads, want)
		}
		if heads := list(t, refs, "refs/lit/log/a"); !slices.Equal(heads, want[1:2]) {
			t.Errorf("List(refs/lit/log/a) = %v; want %v", heads, want[1:2])
		}
	})
	t.Run("a concurrent reader sees a replace whole", func(t *testing.T) {
		refs := fresh(t)
		const name git.RefName = "refs/lit/log/w"
		if err := refs.Replace(name, a); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Go(func() {
			for i := range 200 {
				if err := refs.Replace(name, []git.ObjectID{a, b}[i%2]); err != nil {
					t.Error(err)
					return
				}
			}
		})
		for range 200 {
			heads, bad, err := refs.List("refs/lit/")
			if err != nil || len(bad) != 0 || len(heads) != 1 || heads[0].Target != a && heads[0].Target != b {
				t.Errorf("a reader during replaces saw %v, %v, %v; want the ref at one of its two targets", heads, bad, err)
				break
			}
		}
		wg.Wait()
	})
	t.Run("an id of another format, or the zero id, cannot be a target", func(t *testing.T) {
		foreign, err := git.ParseObjectID(git.SHA256, "6c19f8bd0d8a2b5a6c0b2e8e3c3a0f9e2d1c7b6a5f4e3d2c1b0a9f8e7d6c5b4a")
		if err != nil {
			t.Fatal(err)
		}
		for _, to := range []git.ObjectID{foreign, {}} {
			if err := fresh(t).Replace("refs/lit/log/w", to); !errors.As(err, new(git.ForeignFormat)) {
				t.Errorf("Replace to the %s id %s failed with %v; want ForeignFormat", to.Format(), to, err)
			}
		}
	})
}
