package git

import (
	"encoding/hex"
	"fmt"
	"path"
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
	switch f {
	case SHA1, SHA256:
	default:
		return ObjectID{}, fmt.Errorf("%q cannot be read as an id of %s", s, f)
	}
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != f.size() || hex.EncodeToString(b) != s {
		return ObjectID{}, fmt.Errorf("%q is not a %s object id", s, f)
	}
	id := ObjectID{format: f}
	copy(id.sum[:], b)
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

// BadRef is a ref whose file does not hold an object id: torn by a crash,
// symbolic, or written by something other than lit.
type BadRef struct {
	Name RefName
	Why  error
}

// Objects stores and reads blobs, trees and commits by object id in lit's
// own object database, which is SHA-1 (the store's decision 11). An id of
// another format is ForeignFormat; an id it does not hold is MissingObject.
type Objects interface {
	PutBlob(b []byte) (ObjectID, Stored, error)
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
// replaced, never advanced: only its writer updates it.
type Refs interface {
	// List is every ref whose name starts with prefix, sorted by name,
	// and every such ref that does not hold an object id.
	List(prefix RefName) ([]RefHead, []BadRef, error)
	Replace(name RefName, to ObjectID) error
}

// checkRefName is the one check both of lit's repositories make of a
// name they write, so the one on disk and its twin take the same names
// `[LAW:single-enforcer]`: a clean path under refs/, no component
// starting with . or ending in .lock, which refnames forbid and which
// mark a replace's temporary file and a git command's lock.
func checkRefName(n RefName) error {
	s := string(n)
	bad := checkPrefix(n) != nil || path.Clean(s) != s
	for _, c := range strings.Split(s, "/") {
		bad = bad || strings.HasPrefix(c, ".") || strings.HasSuffix(c, ".lock")
	}
	if bad {
		return fmt.Errorf("%q is not a refname under refs/", n)
	}
	return nil
}

// checkPrefix refuses a list prefix outside refs/.
func checkPrefix(p RefName) error {
	if !strings.HasPrefix(string(p), "refs/") {
		return fmt.Errorf("%q is not a prefix under refs/", p)
	}
	return nil
}

// MissingObject is an id the object database does not hold.
type MissingObject struct{ ID ObjectID }

func (m MissingObject) Error() string {
	return fmt.Sprintf("object %s is not in lit's repository", m.ID)
}

// ForeignFormat is an id of a format lit's repository does not use, the
// zero id's included.
type ForeignFormat struct{ ID ObjectID }

func (f ForeignFormat) Error() string {
	return fmt.Sprintf("%s id %s cannot name an object in lit's sha1 repository", f.ID.format, f.ID)
}
