package git

import (
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// ObjectFormat is the hash function a repository names its objects by.
type ObjectFormat uint8

// The two formats git defines.
const (
	SHA1 ObjectFormat = iota + 1
	SHA256
)

// String names the format; any other value, the zero one an id holds
// before it names an object above all, prints as the illegal value it is,
// since formatting an error must not fail.
func (f ObjectFormat) String() string {
	switch f {
	case SHA1:
		return "sha1"
	case SHA256:
		return "sha256"
	}
	return fmt.Sprintf("no format (%d)", uint8(f))
}

// size is the length of an object id in the format, in bytes.
func (f ObjectFormat) size() int {
	switch f {
	case SHA1:
		return 20
	case SHA256:
		return 32
	}
	panic(fmt.Sprintf("git: %d is not an ObjectFormat", uint8(f)))
}

// ObjectID names one object in one repository. The format is part of the
// id, so a code repository's SHA-256 id and an id in lit's SHA-1 repository
// are equal only when both the format and the bytes agree
// `[LAW:types-are-the-program]`.
type ObjectID struct {
	format ObjectFormat
	sum    [32]byte
}

// ParseObjectID reads an object id written as git writes one: the format's
// length in lowercase hex digits.
func ParseObjectID(f ObjectFormat, s string) (ObjectID, error) {
	b, err := hex.DecodeString(s)
	if err != nil || hex.EncodeToString(b) != s {
		return ObjectID{}, fmt.Errorf("%q is not a %s object id: it is not lowercase hex", s, f)
	}
	id, err := ObjectIDFrom(f, b)
	if err != nil {
		return ObjectID{}, fmt.Errorf("%q is not a %s object id: %w", s, f, err)
	}
	return id, nil
}

// errNoFormat is a format that is neither of git's, so no sum is an id of
// it; errSumSize is a sum not the length its format's ids are.
var (
	errNoFormat = errors.New("no object format")
	errSumSize  = errors.New("a sum not of its format's length")
)

// ObjectIDFrom reads an object id held as its format and its sum, the
// form Sum gives: the sum is the format's length in bytes.
func ObjectIDFrom(f ObjectFormat, sum []byte) (ObjectID, error) {
	switch f {
	case SHA1, SHA256:
	default:
		return ObjectID{}, fmt.Errorf("a sum cannot be read as an id of %s: %w", f, errNoFormat)
	}
	if len(sum) != f.size() {
		return ObjectID{}, fmt.Errorf("a %d-byte sum is not a %s object id: %w", len(sum), f, errSumSize)
	}
	id := ObjectID{format: f}
	copy(id.sum[:], sum)
	return id, nil
}

// Format is the hash function the id was computed with.
func (id ObjectID) Format() ObjectFormat { return id.format }

// Sum is the id's hash, the format's length in bytes. The zero id has no
// format and no sum, and asking for one panics.
func (id ObjectID) Sum() []byte { return slices.Clone(id.sum[:id.format.size()]) }

// String is the id in lowercase hex, as git prints it. The zero id names
// no object and prints as such.
func (id ObjectID) String() string {
	switch id.format {
	case SHA1, SHA256:
		return hex.EncodeToString(id.sum[:id.format.size()])
	}
	return "(no object id)"
}

// Stored says whether a Put added the object or found it already there.
type Stored uint8

// The two answers of a Put.
const (
	New Stored = iota + 1
	Existing
)

// Entry is one name in a lit tree. It holds a blob, so a lit tree cannot
// nest `[LAW:types-are-the-program]`.
type Entry struct {
	Name string
	Blob ObjectID
}

// Commit is a lit commit: parentless, its identity and time fixed by the
// object database, so it is a pure function of its tree and message and
// cannot carry history.
type Commit struct {
	Tree    ObjectID
	Message string
}

// RefName is a git refname, such as refs/lit/log/<checkout-id>.
type RefName string

// RefHead is one ref and the object it names.
type RefHead struct {
	Name   RefName
	Target ObjectID
}

// Listing is the refs under a prefix, each sorted by name: those that name
// an object, and those that are broken.
type Listing struct {
	Heads  []RefHead
	Broken []BrokenRef
}

// BrokenRef is a ref that cannot be read as naming an object, which git
// never writes: its file empty, torn, symbolic or the zero id. A read
// lists it and goes on.
type BrokenRef struct {
	Name RefName
	Why  string
}

func (b BrokenRef) Error() string {
	return fmt.Sprintf("ref %s is broken: %s", b.Name, b.Why)
}

// Put is one object a put names: its id, and whether the put added it.
type Put struct {
	ID     ObjectID
	Stored Stored
}

// Objects stores and reads blobs, trees and commits by object id in lit's
// own object database, which is SHA-1 (the store's decision 11). An id of
// another format is ForeignFormat; an id it does not hold is MissingObject.
type Objects interface {
	// PutBlobs stores every blob of bs in one write, its Put for each in
	// order.
	PutBlobs(bs [][]byte) ([]Put, error)
	Blob(id ObjectID) ([]byte, error)
	// BlobSize reads the object's header only, so listing heads never
	// inflates a segment.
	BlobSize(id ObjectID) (uint64, error)
	PutTree(es []Entry) (ObjectID, Stored, error)
	// Tree is the tree's entries, sorted by name.
	Tree(id ObjectID) ([]Entry, error)
	PutCommit(c Commit) (ObjectID, Stored, error)
	Commit(id ObjectID) (Commit, error)
}

// Refs lists and replaces refs in lit's own object database. A ref is
// replaced, never advanced, and only from what its writer read it naming,
// so a replace that raced another fails rather than undoing it.
type Refs interface {
	// List is every ref under prefix: the ref the prefix names and every
	// ref below it, as git for-each-ref matches a prefix. A broken ref is
	// listed as one, never the listing's failure.
	List(prefix RefName) (Listing, error)
	// Replace points name at to, an object the repository holds, if name
	// still is what from says; otherwise it refuses with Moved, whose Now
	// is the ref's state as read after git refused. A refusal that reads
	// the ref still naming from is git's own failure, Refused, not Moved.
	Replace(name RefName, from Was, to ObjectID) error
}

// Was is what a ref named when its writer read it: an object, or nothing.
//
//sumtype:decl
type Was interface{ was() }

// Named is a ref naming Target.
type Named struct{ Target ObjectID }

// Absent is no ref.
type Absent struct{}

func (Named) was()  {}
func (Absent) was() {}

// String is the object the ref names.
func (n Named) String() string { return n.Target.String() }

// String says there is no ref, so a refusal naming a ref's state reads it
// in words.
func (Absent) String() string { return "absent" }

// name is the one grammar of a tree entry's name and of a refname's
// component in lit's repository: a letter or digit, then letters, digits,
// _ and -. It admits every name lit makes, hex ids and words, and nothing
// git refuses anywhere: no dot, so no ., .., .git or .lock, and none of
// the characters git check-ref-format or fsck forbid
// `[LAW:single-enforcer]`. gittest holds it to git itself.
var name = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// ErrNotAName is a name the name grammar refuses, as a tree entry's name
// or as a refname, which is refs/ and names: one refusal for the one
// grammar.
var ErrNotAName = errors.New("a name not of letters, digits, _ and -")

// ErrDuplicateEntry is a tree given two entries of one name, which git
// fsck refuses.
var ErrDuplicateEntry = errors.New("two tree entries of one name")

// parseRefName refuses a name that is not refs/ and one or more names.
func parseRefName(n RefName) error {
	parts := strings.Split(string(n), "/")
	ok := len(parts) >= 2 && parts[0] == "refs"
	for _, p := range parts[1:] {
		ok = ok && name.MatchString(p)
	}
	if !ok {
		return fmt.Errorf("%q is not a refname lit writes, refs/ and names: %w", n, ErrNotAName)
	}
	return nil
}

// parsePrefix refuses a prefix that is not refs/, a refname, or a refname
// and a slash.
func parsePrefix(p RefName) error {
	if p == "refs/" {
		return nil
	}
	return parseRefName(RefName(strings.TrimSuffix(string(p), "/")))
}

// under says whether n is named by prefix p, as git for-each-ref matches
// one: n is p, or p's next component begins it.
func under(n, p RefName) bool {
	return n == p || strings.HasPrefix(string(n), strings.TrimSuffix(string(p), "/")+"/")
}

// Moved is a ref no longer what its writer read: a replace refused, or a
// publish with nothing to write whose ref has moved since its seal read
// it. From is what the writer read, Now the ref's state as read after the
// refusal, so the writer decides from the refusal itself whether it lost
// a race or won it under another publish.
type Moved struct {
	Name RefName
	From Was
	Now  Was
}

func (m Moved) Error() string { return fmt.Sprintf("%s is %v, not %v", m.Name, m.Now, m.From) }

// MissingObject is an id the object database does not hold.
type MissingObject struct{ ID ObjectID }

func (m MissingObject) Error() string {
	return fmt.Sprintf("object %s is not in lit's repository", m.ID)
}

// NotLit is an object the repository holds that lit did not write: of
// another kind than asked for, ErrWrongKind, or of a shape lit's encoding
// never makes, errForeignShape.
type NotLit struct {
	ID  ObjectID
	Why error
}

func (n NotLit) Error() string { return fmt.Sprintf("object %s is %v", n.ID, n.Why) }

func (n NotLit) Unwrap() error { return n.Why }

// ErrWrongKind is an object of another kind than the one asked for.
var ErrWrongKind = errors.New("an object of another kind")

// errForeignShape is a tree or commit of a shape lit's encoding never
// writes.
var errForeignShape = errors.New("an object of a shape lit never writes")

// ForeignFormat is an id of another format than the repository it is asked
// of, the zero id's included: it names nothing there, so it is refused,
// never answered as an object missing.
type ForeignFormat struct {
	ID         ObjectID
	Repository ObjectFormat
}

func (f ForeignFormat) Error() string {
	return fmt.Sprintf("%s id %s cannot name an object in a %s repository", f.ID.format, f.ID, f.Repository)
}

// inFormat refuses an id that is not of format f.
func inFormat(id ObjectID, f ObjectFormat) error {
	if id.format != f {
		return ForeignFormat{ID: id, Repository: f}
	}
	return nil
}
