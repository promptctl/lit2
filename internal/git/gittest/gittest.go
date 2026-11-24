// Package gittest is the contract suites of lit's repository's two ports,
// Objects and Refs, which the repository on disk and its twin in memory
// both pass, so a test over the twin is a test over the repository.
package gittest

import (
	"errors"
	"slices"
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

// Put stores the one blob b in db, for a test that needs a blob there.
func Put(db git.Objects, b []byte) (git.ObjectID, error) {
	ps, err := db.PutBlobs([][]byte{b})
	if err != nil {
		return git.ObjectID{}, err
	}
	return ps[0].ID, nil
}

// PutBlob is Put, failing the test on an error.
func PutBlob(t testing.TB, db git.Objects, b []byte) git.ObjectID {
	t.Helper()
	id, err := Put(db, b)
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
	// the empty tree, lit's identity at time zero, "lit-log v1 x\n"
	litCommit = "1c7258c2b2e2a8c89f097d114b089681b03a4017"
)

// RefusedEntries is every tree entry name the audit found lit taking where
// git fsck --strict refuses it, and the others lit's grammar refuses, new
// each call. TestTheNameGrammarIsASubsetOfGits holds each to git.
func RefusedEntries() []string {
	return []string{"", ".", "..", ".git", ".GIT", "a/b", "a\x00b", ".hidden", "a b"}
}

// Objects holds fresh, an empty object database each call, to the Objects
// contract.
func Objects(t *testing.T, fresh func(*testing.T) git.Objects) {
	t.Run("a blob's id is git's, and a second put finds it", func(t *testing.T) {
		db := fresh(t)
		for _, c := range []struct {
			content string
			id      string
		}{{"hello\n", helloBlob}, {"", emptyBlob}} {
			ps, err := db.PutBlobs([][]byte{[]byte(c.content)})
			if err != nil || len(ps) != 1 || ps[0] != (git.Put{ID: ID(t, c.id), Stored: git.New}) {
				t.Fatalf("PutBlobs(%q) = %v, %v; want %s, New", c.content, ps, err, c.id)
			}
			id := ps[0].ID
			again, err := db.PutBlobs([][]byte{[]byte(c.content)})
			if err != nil || len(again) != 1 || again[0] != (git.Put{ID: id, Stored: git.Existing}) {
				t.Errorf("a second PutBlobs(%q) = %v, %v; want %s, Existing", c.content, again, err, id)
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
	t.Run("a batch is put in one call: each blob's id in order, and New only for the first of each the repository lacked", func(t *testing.T) {
		db := fresh(t)
		held := PutBlob(t, db, []byte("hello\n"))
		ps, err := db.PutBlobs([][]byte{nil, []byte("hello\n"), nil, []byte("x")})
		want := []git.Put{{ID: ID(t, emptyBlob), Stored: git.New}, {ID: held, Stored: git.Existing}, {ID: ID(t, emptyBlob), Stored: git.Existing}, {ID: git.HashBlob(git.SHA1, []byte("x")), Stored: git.New}}
		if err != nil || !slices.Equal(ps, want) {
			t.Errorf("PutBlobs = %v, %v; want %v", ps, err, want)
		}
		if ps, err := db.PutBlobs(nil); err != nil || len(ps) != 0 {
			t.Errorf("PutBlobs(nothing) = %v, %v; want nothing", ps, err)
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
	t.Run("a repository holds the empty tree, as every git repository does", func(t *testing.T) {
		db := fresh(t)
		if _, err := db.PutBlobs([][]byte{nil}); err != nil {
			t.Fatal(err)
		}
		if es, err := db.Tree(ID(t, emptyTree)); err != nil || len(es) != 0 {
			t.Errorf("Tree(the empty tree) = %v, %v; want no entries", es, err)
		}
	})
	t.Run("a tree reads back sorted, whatever order it was put in", func(t *testing.T) {
		db := fresh(t)
		empty, _, err := db.PutTree(nil)
		if err != nil || empty != ID(t, emptyTree) {
			t.Errorf("PutTree(nil) = %v, %v; want git's empty tree %s", empty, err, emptyTree)
		}
		hello, nothing := PutBlob(t, db, []byte("hello\n")), PutBlob(t, db, nil)
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
		blob := PutBlob(t, db, []byte("x"))
		for _, es := range [][]git.Entry{{{Name: "a", Blob: blob}, {Name: "a", Blob: blob}}} {
			if _, _, err := db.PutTree(es); err == nil {
				t.Errorf("PutTree(%v) stored a tree", es)
			}
		}
		for _, n := range RefusedEntries() {
			if _, _, err := db.PutTree([]git.Entry{{Name: n, Blob: blob}}); err == nil {
				t.Errorf("PutTree(%q) stored a tree", n)
			}
		}
	})
	t.Run("an object of one kind is never read or named as another", func(t *testing.T) {
		db := fresh(t)
		blob := PutBlob(t, db, []byte("x"))
		tree, _, _ := db.PutTree([]git.Entry{{Name: "a", Blob: blob}})
		commit, _, _ := db.PutCommit(git.Commit{Tree: tree, Message: "m\n"})
		_, errBlob := db.Blob(tree)
		_, errSize := db.BlobSize(commit)
		_, errTree := db.Tree(blob)
		_, errCommit := db.Commit(tree)
		_, _, errEntry := db.PutTree([]git.Entry{{Name: "a", Blob: tree}})
		_, _, errOver := db.PutCommit(git.Commit{Tree: blob})
		for i, err := range []error{errBlob, errSize, errTree, errCommit, errEntry, errOver} {
			if err == nil || errors.As(err, new(git.MissingObject)) {
				t.Errorf("case %d: an object of the wrong kind was taken with %v", i, err)
			}
		}
		_, _, errAbsent := db.PutTree([]git.Entry{{Name: "a", Blob: ID(t, helloBlob)}})
		_, _, errNoTree := db.PutCommit(git.Commit{Tree: ID(t, helloBlob)})
		for _, err := range []error{errAbsent, errNoTree} {
			if !errors.As(err, new(git.MissingObject)) {
				t.Errorf("naming an absent object failed with %v; want MissingObject", err)
			}
		}
	})
	t.Run("a commit is a pure function of its tree and message", func(t *testing.T) {
		db := fresh(t)
		tree, _, _ := db.PutTree(nil)
		c := git.Commit{Tree: tree, Message: "lit-log v1 x\n"}
		id, stored, err := db.PutCommit(c)
		if err != nil || stored != git.New || id != ID(t, litCommit) {
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

// Repository is both of lit's repository's ports, as the disk repository
// and its twin each are.
type Repository interface {
	git.Objects
	git.Refs
}

// Refused is every refname the 2026-11-19 audit found lit taking where git
// refuses it, and the names git refuses for a lock, a temporary file or a
// path that is not clean, new each call. TestTheNameGrammarIsASubsetOfGits
// holds each to git check-ref-format.
func Refused() []git.RefName {
	return []git.RefName{"HEAD", "lit/log/w", "refs", "refs/lit/../w", "refs/lit//w", "refs/lit/w/",
		"refs/lit/.w", "refs/lit/.ref-1", "refs/lit/w.lock", "refs/lit/log/a..b", "refs/lit/a b", "refs/lit/a:b",
		"refs/lit/a~b", "refs/lit/a^b", "refs/lit/a?b", "refs/lit/a*b", "refs/lit/a[b", "refs/lit/a\\b",
		"refs/lit/a@{b", "refs/lit/a.", "refs/lit/a\x00b"}
}

// Refs holds fresh, an empty repository each call, to the Refs contract.
func Refs(t *testing.T, fresh func(*testing.T) Repository) {
	objects := func(t *testing.T, r Repository) (a, b git.ObjectID) {
		t.Helper()
		return PutBlob(t, r, []byte("hello\n")), PutBlob(t, r, nil)
	}
	// list is the refs under prefix, every one whole, as lit's own writes
	// leave them
	list := func(t *testing.T, refs git.Refs, prefix git.RefName) []git.RefHead {
		t.Helper()
		l, err := refs.List(prefix)
		if err != nil || len(l.Removed) != 0 {
			t.Fatalf("List(%s) = %+v, %v", prefix, l, err)
		}
		return l.Heads
	}
	t.Run("a fresh repository holds no ref", func(t *testing.T) {
		if heads := list(t, fresh(t), "refs/lit/"); len(heads) != 0 {
			t.Errorf("a fresh repository lists %v", heads)
		}
	})
	t.Run("a replace is listed, and replaced again", func(t *testing.T) {
		refs := fresh(t)
		a, b := objects(t, refs)
		const name git.RefName = "refs/lit/log/w"
		was := git.Was(git.Absent{})
		for _, to := range []git.ObjectID{a, b} {
			if err := refs.Replace(name, was, to); err != nil {
				t.Fatal(err)
			}
			if heads := list(t, refs, "refs/lit/"); !slices.Equal(heads, []git.RefHead{{Name: name, Target: to}}) {
				t.Errorf("after Replace(%s, %s) the refs are %v", name, to, heads)
			}
			was = git.Named{Target: to}
		}
	})
	t.Run("a replace from what the ref no longer is is refused, so a racing writer is never undone", func(t *testing.T) {
		refs := fresh(t)
		a, b := objects(t, refs)
		const name git.RefName = "refs/lit/log/w"
		if err := refs.Replace(name, git.Absent{}, a); err != nil {
			t.Fatal(err)
		}
		for _, stale := range []git.Was{git.Absent{}, git.Named{Target: b}} {
			if err := refs.Replace(name, stale, b); err == nil {
				t.Errorf("Replace(%s) from %v, while it names %s, = nil; want a refusal", name, stale, a)
			}
		}
		if err := refs.Replace("refs/lit/log/v", git.Named{Target: a}, b); err == nil {
			t.Errorf("Replace of an absent ref from %s = nil; want a refusal", a)
		}
		if heads := list(t, refs, "refs/lit/"); !slices.Equal(heads, []git.RefHead{{Name: name, Target: a}}) {
			t.Errorf("after refused replaces the refs are %v; want %s at %s alone", heads, name, a)
		}
	})
	t.Run("a name git refuses is refused, and so is a prefix", func(t *testing.T) {
		refs := fresh(t)
		a, _ := objects(t, refs)
		for _, name := range Refused() {
			if err := refs.Replace(name, git.Absent{}, a); err == nil {
				t.Errorf("Replace(%q) = nil; want a refusal", name)
			}
		}
		for _, prefix := range []git.RefName{"refs/../", "lit/", "refs/lit/*", "refs/lit//"} {
			if heads, err := refs.List(prefix); err == nil {
				t.Errorf("List(%q) = %v; want a refusal", prefix, heads)
			}
		}
		if heads := list(t, refs, "refs/"); len(heads) != 0 {
			t.Errorf("after refused replaces the refs are %v", heads)
		}
	})
	t.Run("a list is sorted and holds only what its prefix names, as git for-each-ref matches it", func(t *testing.T) {
		refs := fresh(t)
		a, _ := objects(t, refs)
		for _, name := range []git.RefName{"refs/lit/log/b", "refs/lit/inbox/c", "refs/lit/log/a", "refs/other/d", "refs/litter/x", "refs/lit/log/ab"} {
			if err := refs.Replace(name, git.Absent{}, a); err != nil {
				t.Fatal(err)
			}
		}
		want := []git.RefHead{{Name: "refs/lit/inbox/c", Target: a}, {Name: "refs/lit/log/a", Target: a}, {Name: "refs/lit/log/ab", Target: a}, {Name: "refs/lit/log/b", Target: a}}
		for _, prefix := range []git.RefName{"refs/lit/", "refs/lit"} {
			if heads := list(t, refs, prefix); !slices.Equal(heads, want) {
				t.Errorf("List(%s) = %v; want %v", prefix, heads, want)
			}
		}
		if heads := list(t, refs, "refs/lit/log/a"); !slices.Equal(heads, want[1:2]) {
			t.Errorf("List(refs/lit/log/a) = %v; want %v", heads, want[1:2])
		}
	})
	t.Run("a ref cannot be the directory of another, nor have one as its directory", func(t *testing.T) {
		refs := fresh(t)
		a, _ := objects(t, refs)
		if err := refs.Replace("refs/lit/a", git.Absent{}, a); err != nil {
			t.Fatal(err)
		}
		if err := refs.Replace("refs/lit/a/b", git.Absent{}, a); err == nil {
			t.Errorf("refs/lit/a/b was taken beside refs/lit/a")
		}
		if err := refs.Replace("refs/lit/b/c", git.Absent{}, a); err != nil {
			t.Fatal(err)
		}
		if err := refs.Replace("refs/lit/b", git.Absent{}, a); err == nil {
			t.Errorf("refs/lit/b was taken beside refs/lit/b/c")
		}
	})
	t.Run("a target the repository does not hold is missing", func(t *testing.T) {
		refs := fresh(t)
		if err := refs.Replace("refs/lit/log/w", git.Absent{}, ID(t, helloBlob)); !errors.As(err, new(git.MissingObject)) {
			t.Errorf("Replace to an absent object failed with %v; want MissingObject", err)
		}
	})
	t.Run("an id of another format, or the zero id, cannot be a target", func(t *testing.T) {
		foreign, err := git.ParseObjectID(git.SHA256, "6c19f8bd0d8a2b5a6c0b2e8e3c3a0f9e2d1c7b6a5f4e3d2c1b0a9f8e7d6c5b4a")
		if err != nil {
			t.Fatal(err)
		}
		for _, to := range []git.ObjectID{foreign, {}} {
			if err := fresh(t).Replace("refs/lit/log/w", git.Absent{}, to); !errors.As(err, new(git.ForeignFormat)) {
				t.Errorf("Replace to the %s id %s failed with %v; want ForeignFormat", to.Format(), to, err)
			}
		}
	})
}
