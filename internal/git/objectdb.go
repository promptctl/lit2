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

// encode writes each entry as git does: its mode, a space, its name, a NUL
// and its blob's sum. Every entry is a regular file.
func (t tree) encode() (kind, []byte) {
	var b []byte
	for _, e := range t {
		b = append(append(append(b, "100644 "+e.Name...), 0), e.Blob.Sum()...)
	}
	return treeKind, b
}

// identity is every lit commit's author and committer line: fixed, at time
// zero, so a commit is a pure function of its tree and message, publish
// reads no clock, and racing publishes of one log converge on one id
// `[LAW:effects-at-boundaries]`. litEnv hands git the same identity.
const identity = "author lit <lit> 0 +0000\ncommitter lit <lit> 0 +0000\n"

func (c commit) encode() (kind, []byte) {
	return commitKind, []byte("tree " + c.Tree.String() + "\n" + identity + "\n" + c.Message)
}

func idOf(o object) ObjectID {
	k, b := o.encode()
	return hashObject(SHA1, k, b)
}

// backend is where a repository's objects and refs are kept: git, or the
// twin's maps. Everything a repository checks, it checks above this, once
// for both `[LAW:one-type-per-behavior]`.
type backend interface {
	// look is the kind and size of the object id names, or MissingObject.
	look(id ObjectID) (kind, uint64, error)
	// read is its kind and bytes, or MissingObject.
	read(id ObjectID) (kind, []byte, error)
	// write stores o and is the id the backend gave it.
	write(o object) (ObjectID, error)
	list(prefix RefName) ([]RefHead, error)
	replace(name RefName, to ObjectID) error
}

// repo is lit's repository over a backend: its Objects and Refs.
type repo struct{ b backend }

func (r repo) PutBlob(b []byte) (ObjectID, Stored, error) { return r.put(blob(b)) }

func (r repo) Blob(id ObjectID) ([]byte, error) { return r.content(blobKind, id) }

// BlobSize answers for a blob alone, so a tree naming another kind of
// object never passes as one.
func (r repo) BlobSize(id ObjectID) (uint64, error) { return r.holds(blobKind, id) }

// PutTree stores es as a tree, sorted by name as git requires. Each entry's
// name is one the name grammar admits, no two alike, and each holds a blob
// the repository has.
func (r repo) PutTree(es []Entry) (ObjectID, Stored, error) {
	sorted := slices.SortedFunc(slices.Values(es), func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	for i, e := range sorted {
		if !name.MatchString(e.Name) {
			return ObjectID{}, 0, fmt.Errorf("%q cannot name a tree entry: a name is letters, digits, _ and -", e.Name)
		}
		if i > 0 && sorted[i-1].Name == e.Name {
			return ObjectID{}, 0, fmt.Errorf("two tree entries are named %q", e.Name)
		}
		if _, err := r.holds(blobKind, e.Blob); err != nil {
			return ObjectID{}, 0, err
		}
	}
	return r.put(tree(sorted))
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
		if string(mode) != "100644" || !name.Match(n) || len(after) < 20 {
			return nil, fmt.Errorf("tree %s: not a lit tree", id)
		}
		blob, _ := ObjectIDFrom(SHA1, after[:20])
		es, rest = append(es, Entry{Name: string(n), Blob: blob}), after[20:]
	}
	if _, again := tree(es).encode(); !bytes.Equal(again, b) || !slices.IsSortedFunc(es, func(a, b Entry) int { return strings.Compare(a.Name, b.Name) }) {
		return nil, fmt.Errorf("tree %s: not a lit tree", id)
	}
	return es, nil
}

// PutCommit stores c as a lit commit over a tree the repository has.
func (r repo) PutCommit(c Commit) (ObjectID, Stored, error) {
	if _, err := r.holds(treeKind, c.Tree); err != nil {
		return ObjectID{}, 0, err
	}
	return r.put(commit(c))
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
		return Commit{}, fmt.Errorf("commit %s: not a lit commit", id)
	}
	return Commit{Tree: t, Message: msg}, nil
}

// List is every ref under prefix, sorted by name.
func (r repo) List(prefix RefName) ([]RefHead, error) {
	if err := parsePrefix(prefix); err != nil {
		return nil, err
	}
	return r.b.list(prefix)
}

// Replace points name at to, an object the repository holds.
func (r repo) Replace(name RefName, to ObjectID) error {
	if err := parseRefName(name); err != nil {
		return err
	}
	if err := own(to); err != nil {
		return err
	}
	if _, _, err := r.b.look(to); err != nil {
		return err
	}
	return r.b.replace(name, to)
}

// put stores o unless the repository holds it already; content addressing
// makes a second write of one object the same object. The backend's id
// must be the one lit computed: git is the oracle of every id lit names.
func (r repo) put(o object) (ObjectID, Stored, error) {
	id := idOf(o)
	_, _, err := r.b.look(id)
	if err == nil {
		return id, Existing, nil
	}
	if !errors.As(err, new(MissingObject)) {
		return ObjectID{}, 0, err
	}
	got, err := r.b.write(o)
	if err != nil {
		return ObjectID{}, 0, err
	}
	if got != id {
		k, _ := o.encode()
		return ObjectID{}, 0, fmt.Errorf("git stored a %s as %s; lit computes %s", k, got, id)
	}
	return id, New, nil
}

// holds is the size of the object of kind k id names, refusing an id the
// repository does not hold as one.
func (r repo) holds(k kind, id ObjectID) (uint64, error) {
	if err := own(id); err != nil {
		return 0, err
	}
	got, size, err := r.b.look(id)
	if err == nil && got != k {
		err = fmt.Errorf("object %s is a %s, not a %s", id, got, k)
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
		return nil, fmt.Errorf("object %s is a %s, not a %s", id, got, k)
	}
	return b, nil
}

// own refuses an id of another format, or the zero id: lit's repository is
// SHA-1, so neither names anything in it.
func own(id ObjectID) error {
	if id.format != SHA1 {
		return ForeignFormat{ID: id}
	}
	return nil
}
