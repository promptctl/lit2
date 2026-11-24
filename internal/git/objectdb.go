package git

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// kind is an object's type as git names it.
type kind string

const (
	blobKind   kind = "blob"
	treeKind   kind = "tree"
	commitKind kind = "commit"
)

// object is one object lit puts: its kind and bytes are lit's encoding,
// which is git's, so its id is computed before any write.
//
//sumtype:decl
type object interface{ encode() (kind, []byte) }

type (
	blob []byte
	// tree is a lit tree's entries, sorted by name.
	tree   []Entry
	commit Commit
)

func (b blob) encode() (kind, []byte) { return blobKind, b }

// fileMode is the mode of every entry in a lit tree: a regular file.
const fileMode = "100644"

// encode writes each entry as git does: its mode, a space, its name, a NUL
// and its blob's sum.
func (t tree) encode() (kind, []byte) {
	var b []byte
	for _, e := range t {
		b = append(append(append(b, fileMode+" "+e.Name...), 0), e.Blob.Sum()...)
	}
	return treeKind, b
}

// byName is git's order of a tree's entries, which lit's names, holding no
// / and no byte past ASCII, share with plain byte order.
func byName(a, b Entry) int { return strings.Compare(a.Name, b.Name) }

// identity is every lit commit's author and committer line: fixed, at time
// zero, so a commit is a pure function of its tree and message, publish
// reads no clock, and racing publishes of one log converge on one id
// `[LAW:effects-at-boundaries]`.
const identity = "author lit <lit> 0 +0000\ncommitter lit <lit> 0 +0000\n"

func (c commit) encode() (kind, []byte) {
	return commitKind, []byte("tree " + c.Tree.String() + "\n" + identity + "\n" + c.Message)
}

func idOf(o object) ObjectID {
	k, b := o.encode()
	return hashObject(SHA1, k, b)
}

// backend is where a repository's objects and refs are kept: git, or the
// twin's maps. Every rule of lit's own, its names, kinds and shapes, a
// repository checks above this, once for both
// `[LAW:one-type-per-behavior]`; a rule of git's, that a ref cannot sit
// beside its own directory or move from what it no longer names, git
// enforces for the disk and the twin copies below.
type backend interface {
	// look is the kind and size of the object id names, or MissingObject.
	look(id ObjectID) (kind, uint64, error)
	// read is its kind and bytes, or MissingObject.
	read(id ObjectID) (kind, []byte, error)
	// write stores objs and is the id the backend gave each, in order: in
	// one step for any number of them, so a publish's cost in git
	// processes does not grow with what it writes.
	write(objs []object) ([]ObjectID, error)
	list(prefix RefName) (Listing, error)
	// replace points name at to if it still is what from says.
	replace(name RefName, from Was, to ObjectID) error
}

// repo is lit's repository over a backend: its Objects and Refs.
type repo struct{ b backend }

func (r repo) PutBlobs(bs [][]byte) ([]Put, error) {
	objs := make([]object, len(bs))
	for i, b := range bs {
		objs[i] = blob(b)
	}
	return r.put(objs)
}

func (r repo) Blob(id ObjectID) ([]byte, error) { return r.content(blobKind, id) }

// BlobSize answers for a blob alone, so a tree naming another kind of
// object never passes as one.
func (r repo) BlobSize(id ObjectID) (uint64, error) { return r.holds(blobKind, id) }

// PutTree stores es as a tree, sorted by name as git requires. Each entry's
// name is one the name grammar admits, no two alike, and each holds a blob
// the repository has.
func (r repo) PutTree(es []Entry) (ObjectID, Stored, error) {
	sorted := slices.SortedFunc(slices.Values(es), byName)
	for i, e := range sorted {
		if !name.MatchString(e.Name) {
			return ObjectID{}, 0, fmt.Errorf("%q cannot name a tree entry: %w", e.Name, ErrNotAName)
		}
		if i > 0 && sorted[i-1].Name == e.Name {
			return ObjectID{}, 0, fmt.Errorf("%w: %q", ErrDuplicateEntry, e.Name)
		}
		if _, err := r.holds(blobKind, e.Blob); err != nil {
			return ObjectID{}, 0, err
		}
	}
	return r.putOne(tree(sorted))
}

// Tree reads a lit tree: what lit's encoding writes, and nothing else, so
// a tree lit did not write is refused rather than read as less than it is.
func (r repo) Tree(id ObjectID) ([]Entry, error) {
	b, err := r.content(treeKind, id)
	if err != nil {
		return nil, err
	}
	es := []Entry{}
	for rest := b; len(rest) > 0; {
		mode, after, _ := bytes.Cut(rest, []byte(" "))
		n, after, _ := bytes.Cut(after, []byte{0})
		if string(mode) != fileMode || !name.Match(n) || len(after) < 20 {
			return nil, NotLit{ID: id, Why: fmt.Errorf("a tree %w", errForeignShape)}
		}
		blob, _ := ObjectIDFrom(SHA1, after[:20])
		es, rest = append(es, Entry{Name: string(n), Blob: blob}), after[20:]
	}
	// the entries' bytes are all read above, so only their order is left
	for i := 1; i < len(es); i++ {
		if byName(es[i-1], es[i]) >= 0 {
			return nil, NotLit{ID: id, Why: fmt.Errorf("a tree %w", errForeignShape)}
		}
	}
	return es, nil
}

// PutCommit stores c as a lit commit over a tree the repository has.
func (r repo) PutCommit(c Commit) (ObjectID, Stored, error) {
	if _, err := r.holds(treeKind, c.Tree); err != nil {
		return ObjectID{}, 0, err
	}
	return r.putOne(commit(c))
}

// Commit reads a lit commit: its tree, lit's identity at time zero, no
// parent, no signature, then the message. Anything else is a commit lit
// did not write, refused, so a read commit puts back as the same id.
func (r repo) Commit(id ObjectID) (Commit, error) {
	b, err := r.content(commitKind, id)
	if err != nil {
		return Commit{}, err
	}
	s, ok := strings.CutPrefix(string(b), "tree ")
	tree, rest, _ := strings.Cut(s, "\n")
	msg, okShape := strings.CutPrefix(rest, identity+"\n")
	t, err := ParseObjectID(SHA1, tree)
	if !ok || !okShape || err != nil {
		return Commit{}, NotLit{ID: id, Why: fmt.Errorf("a commit %w", errForeignShape)}
	}
	return Commit{Tree: t, Message: msg}, nil
}

// List is every ref under prefix.
func (r repo) List(prefix RefName) (Listing, error) {
	if err := parsePrefix(prefix); err != nil {
		return Listing{}, err
	}
	return r.b.list(prefix)
}

// Replace points name at to, an object the repository holds, if name still
// is what from says.
func (r repo) Replace(name RefName, from Was, to ObjectID) error {
	if err := parseRefName(name); err != nil {
		return err
	}
	if err := own(to); err != nil {
		return err
	}
	if _, _, err := r.b.look(to); err != nil {
		return err
	}
	return r.b.replace(name, from, to)
}

func (r repo) putOne(o object) (ObjectID, Stored, error) {
	ps, err := r.put([]object{o})
	if err != nil {
		return ObjectID{}, 0, err
	}
	return ps[0].ID, ps[0].Stored, nil
}

// put stores each of objs the repository does not hold already, all in one
// write; content addressing makes a second write of one object the same
// object. The backend's ids must be the ones lit computed: git is the
// oracle of every id lit names.
func (r repo) put(objs []object) ([]Put, error) {
	ps := make([]Put, len(objs))
	var fresh []object
	var at []int
	queued := map[ObjectID]bool{}
	for i, o := range objs {
		ps[i] = Put{ID: idOf(o), Stored: Existing}
		if queued[ps[i].ID] {
			continue
		}
		_, _, err := r.b.look(ps[i].ID)
		if errors.As(err, new(MissingObject)) {
			ps[i].Stored, queued[ps[i].ID] = New, true
			fresh, at = append(fresh, o), append(at, i)
			continue
		}
		if err != nil {
			return nil, err
		}
	}
	got, err := r.b.write(fresh)
	if err != nil {
		return nil, err
	}
	for j, id := range got {
		if want := ps[at[j]].ID; id != want {
			k, _ := fresh[j].encode()
			return nil, fmt.Errorf("git stored a %s as %s; lit computes %s", k, id, want)
		}
	}
	return ps, nil
}

// holds is the size of the object of kind k id names, refusing an id the
// repository does not hold as one.
func (r repo) holds(k kind, id ObjectID) (uint64, error) {
	if err := own(id); err != nil {
		return 0, err
	}
	got, size, err := r.b.look(id)
	if err == nil && got != k {
		err = NotLit{ID: id, Why: fmt.Errorf("%w: a %s, not a %s", ErrWrongKind, got, k)}
	}
	return size, err
}

func (r repo) content(k kind, id ObjectID) ([]byte, error) {
	if err := own(id); err != nil {
		return nil, err
	}
	got, b, err := r.b.read(id)
	if err != nil {
		return nil, err
	}
	if got != k {
		return nil, NotLit{ID: id, Why: fmt.Errorf("%w: a %s, not a %s", ErrWrongKind, got, k)}
	}
	return b, nil
}

// own refuses an id of another format, or the zero id: lit's repository is
// SHA-1, so neither names anything in it.
func own(id ObjectID) error { return inFormat(id, SHA1) }
