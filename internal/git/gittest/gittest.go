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

// PutTree stores the tree es in db, failing the test on an error.
func PutTree(t testing.TB, db git.Objects, es []git.Entry) git.ObjectID {
	t.Helper()
	id, _, err := db.PutTree(es)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// PutCommit stores the commit c in db, failing the test on an error.
func PutCommit(t testing.TB, db git.Objects, c git.Commit) git.ObjectID {
	t.Helper()
	id, _, err := db.PutCommit(c)
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
		// the control: one entry of a name the grammar takes is stored, so
		// each refusal below is its name's
		PutTree(t, db, []git.Entry{{Name: "a", Blob: blob}})
		twice := []git.Entry{{Name: "a", Blob: blob}, {Name: "a", Blob: blob}}
		if _, _, err := db.PutTree(twice); !errors.Is(err, git.ErrDuplicateEntry) {
			t.Errorf("PutTree(%v) = %v; want ErrDuplicateEntry", twice, err)
		}
		for _, n := range RefusedEntries() {
			if _, _, err := db.PutTree([]git.Entry{{Name: n, Blob: blob}}); !errors.Is(err, git.ErrNotAName) {
				t.Errorf("PutTree(%q) = %v; want ErrNotAName", n, err)
			}
		}
	})
	t.Run("an object of one kind is never read or named as another", func(t *testing.T) {
		db := fresh(t)
		blob := PutBlob(t, db, []byte("x"))
		// putting the tree over the blob and the commit over the tree is
		// the control of the last two cases, each object named as its own kind
		tree := PutTree(t, db, []git.Entry{{Name: "a", Blob: blob}})
		commit := PutCommit(t, db, git.Commit{Tree: tree, Message: "m\n"})
		for name, read := range map[string]struct{ own, other func() error }{
			"Blob":     {func() error { _, err := db.Blob(blob); return err }, func() error { _, err := db.Blob(tree); return err }},
			"BlobSize": {func() error { _, err := db.BlobSize(blob); return err }, func() error { _, err := db.BlobSize(commit); return err }},
			"Tree":     {func() error { _, err := db.Tree(tree); return err }, func() error { _, err := db.Tree(blob); return err }},
			"Commit":   {func() error { _, err := db.Commit(commit); return err }, func() error { _, err := db.Commit(tree); return err }},
		} {
			if err := read.own(); err != nil {
				t.Errorf("%s of its own kind failed with %v", name, err)
			}
			if err := read.other(); !errors.Is(err, git.ErrWrongKind) || !errors.As(err, new(git.NotLit)) {
				t.Errorf("%s of another kind failed with %v; want NotLit, ErrWrongKind", name, err)
			}
		}
		_, _, errEntry := db.PutTree([]git.Entry{{Name: "a", Blob: tree}})
		_, _, errOver := db.PutCommit(git.Commit{Tree: blob})
		for i, err := range []error{errEntry, errOver} {
			if !errors.Is(err, git.ErrWrongKind) {
				t.Errorf("case %d: naming an object of the wrong kind failed with %v; want ErrWrongKind", i, err)
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
		tree := PutTree(t, db, nil)
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
		if other := PutCommit(t, db, git.Commit{Tree: tree, Message: "lit-log v1 y\n"}); other == id {
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
				if !errors.As(err, new(git.ForeignFormat)) {
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
		if err != nil || len(l.Broken) != 0 {
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
		for _, c := range []struct {
			name git.RefName
			from git.Was
			now  git.Was
		}{
			{name, git.Absent{}, git.Named{Target: a}},
			{name, git.Named{Target: b}, git.Named{Target: a}},
			{"refs/lit/log/v", git.Named{Target: a}, git.Absent{}},
		} {
			var moved git.Moved
			err := refs.Replace(c.name, c.from, b)
			if !errors.As(err, &moved) || moved != (git.Moved{Name: c.name, From: c.from, Now: c.now}) {
				t.Errorf("Replace(%s) from %v, while it is %v, = %v; want Moved from %v, now %v", c.name, c.from, c.now, err, c.from, c.now)
			}
		}
		if heads := list(t, refs, "refs/lit/"); !slices.Equal(heads, []git.RefHead{{Name: name, Target: a}}) {
			t.Errorf("after refused replaces the refs are %v; want %s at %s alone", heads, name, a)
		}
	})
	t.Run("a name git refuses is refused, and so is a prefix", func(t *testing.T) {
		refs := fresh(t)
		a, _ := objects(t, refs)
		for _, name := range Refused() {
			if err := refs.Replace(name, git.Absent{}, a); !errors.Is(err, git.ErrNotAName) {
				t.Errorf("Replace(%q) = %v; want ErrNotAName", name, err)
			}
		}
		for _, prefix := range []git.RefName{"refs/../", "lit/", "refs/lit/*", "refs/lit//"} {
			if heads, err := refs.List(prefix); !errors.Is(err, git.ErrNotAName) {
				t.Errorf("List(%q) = %v, %v; want ErrNotAName", prefix, heads, err)
			}
		}
		// the control: a name the grammar takes is replaced, and a prefix
		// it takes lists it, alone among every name above
		const taken git.RefName = "refs/lit/log/w"
		if err := refs.Replace(taken, git.Absent{}, a); err != nil {
			t.Fatalf("Replace(%s) = %v", taken, err)
		}
		if heads := list(t, refs, "refs/"); !slices.Equal(heads, []git.RefHead{{Name: taken, Target: a}}) {
			t.Errorf("after refused replaces and %s the refs are %v; want %s alone", taken, heads, taken)
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
		for _, name := range []git.RefName{"refs/lit/a", "refs/lit/b/c"} {
			if err := refs.Replace(name, git.Absent{}, a); err != nil {
				t.Fatal(err)
			}
		}
		// This refusal is git's alone on disk, so it has no identity of
		// lit's: each refused call is paired with the same call in the same
		// state, its name alone changed so no ref is its directory or under
		// it, which is taken.
		for _, c := range []struct{ refused, taken git.RefName }{
			{"refs/lit/a/b", "refs/lit/d/b"},
			{"refs/lit/b", "refs/lit/e"},
		} {
			before := list(t, refs, "refs/")
			if err := refs.Replace(c.refused, git.Absent{}, a); err == nil {
				t.Errorf("%s was taken beside a ref it is the directory of, or under", c.refused)
			}
			if after := list(t, refs, "refs/"); !slices.Equal(after, before) {
				t.Errorf("the refused Replace(%s) changed the refs from %v to %v", c.refused, before, after)
			}
			if err := refs.Replace(c.taken, git.Absent{}, a); err != nil {
				t.Errorf("Replace(%s), %s with only its name changed, = %v", c.taken, c.refused, err)
			}
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
