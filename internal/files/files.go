// Package files is the files of one directory: appended, created once,
// read and listed. A name holds one path element, so no port reaches a
// subdirectory of its directory, nor, by spelling, a file of a port over
// any other directory `[LAW:decomposition]`.
//
// Its layer in ARCHITECTURE.md: below git, importing nothing of lit2's.
// One Files port has an adapter on disk, At, and a twin in memory, Memory.
package files

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
)

// Name is a file's name in its directory: one path element, never empty,
// . or .., holding no separator or NUL, and never starting with the
// prefix a create-once's temporary files take. Only NameOf, MustName and
// a listing make one `[LAW:parse-dont-validate]`.
type Name struct{ s string }

// ErrNotAName is text that is no one file's name in a directory.
var ErrNotAName = errors.New("not a file's name in one directory")

// tempPrefix starts the name of every temporary file CreateOnce writes,
// and no Name, so a temp is never a file of the port.
const tempPrefix = ".tmp-"

// NameOf is s as a name, refused with ErrNotAName when it is empty, . or
// .., holds /, \ or NUL, any of which would reach outside the directory
// or name none of its files, or starts with the temporary files' prefix.
func NameOf(s string) (Name, error) {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "/\\\x00") || strings.HasPrefix(s, tempPrefix) {
		return Name{}, fmt.Errorf("%q: %w", s, ErrNotAName)
	}
	return Name{s: s}, nil
}

// MustName is the fixed name s; a name written in the source that NameOf
// refuses is a bug, so it panics.
func MustName(s string) Name {
	n, err := NameOf(s)
	if err != nil {
		panic(fmt.Errorf("files: %w", err))
	}
	return n
}

// String is the name as its directory holds it.
func (n Name) String() string { return n.s }

// valid refuses the zero Name, the one Name no constructor makes; each
// implementation asks it first of every name it is given.
func (n Name) valid() error {
	if n.s == "" {
		return fmt.Errorf("the zero Name: %w", ErrNotAName)
	}
	return nil
}

// ErrNoDir is the zero Dir, which names no directory.
var ErrNoDir = errors.New("the zero Dir, which names no directory: a Dir is made by DirAt")

// ErrNotABase is a base given DirAt that is not an absolute path. A base
// is a directory lit2 or git knows, so one that is empty or relative to
// whatever the working directory is is a bug.
var ErrNotABase = errors.New("not an absolute path to start a Dir from")

// Dir is a directory a Files is over: a base lit2 or git knows, trusted,
// and the elements under it, each a directory checked in turn from the
// base, so an element that is not the real directory at its name, a
// symlink among them, is refused.
type Dir struct {
	base  string
	elems []Name
	// gate runs between the step's look at each entry and its open, given
	// the entry's path: ungated, but in a Dir files' own tests gate, a
	// swap of the entry, the race the step's checks of what it opened close
	gate func(path string)
}

// ungated is the gate of every Dir but a test's: nothing.
func ungated(string) {}

// DirAt is the directory first in base, which must be absolute; a base
// that is not is a bug, so it panics `[LAW:parse-dont-validate]`.
func DirAt(base string, first Name) Dir {
	if !filepath.IsAbs(base) {
		panic(fmt.Errorf("files: DirAt(%q): %w", base, ErrNotABase))
	}
	return Dir{base: base, elems: []Name{first}, gate: ungated}
}

// Sub is the directory n in d.
func (d Dir) Sub(n Name) Dir {
	return Dir{base: d.base, elems: append(slices.Clone(d.elems), n), gate: d.gate}
}

// ErrNoParent is the parent asked of a Dir of one element, whose parent is
// its base, a path, which no Dir names.
var ErrNoParent = errors.New("no Dir is the parent of a Dir of one element")

// Parent is the directory d is in: d without its last element. Asking it
// of a Dir of one element is a bug, so it panics.
func (d Dir) Parent() Dir {
	if len(d.elems) < 2 {
		panic(fmt.Errorf("files: the parent of %s: %w", d.Path(), ErrNoParent))
	}
	return Dir{base: d.base, elems: slices.Clone(d.elems[:len(d.elems)-1]), gate: d.gate}
}

// Path is the directory's path, as a message names it.
func (d Dir) Path() string { return d.upTo(len(d.elems)) }

// PathOf is the path of the file n in d, as a message names it.
func (d Dir) PathOf(n Name) string { return filepath.Join(d.Path(), n.s) }

// upTo is the path of d's first i elements under its base.
func (d Dir) upTo(i int) string {
	parts := []string{d.base}
	for _, e := range d.elems[:i] {
		parts = append(parts, e.s)
	}
	return filepath.Join(parts...)
}

// valid refuses the zero Dir, the one Dir DirAt never makes, and one with
// a zero element.
func (d Dir) valid() error {
	if d.base == "" {
		return ErrNoDir
	}
	for _, e := range d.elems {
		if err := e.valid(); err != nil {
			return err
		}
	}
	return nil
}

// Files appends to, creates once, reads and lists the plain files of one
// directory.
type Files interface {
	// Append writes b at the end of the file in one write on an O_APPEND
	// descriptor, then fsyncs, and returns the file's offset after it; so
	// concurrent appends land whole with no lock. A missing file is
	// NotFound.
	Append(n Name, b []byte) (end uint64, err error)
	// CreateOnce creates the file holding b unless it exists: b is written
	// to a temporary file, fsynced and linked into place, so exactly one
	// creation wins and the file is never seen, nor left by a crash,
	// partly written. On disk this needs hard links, since link(2) is the
	// one call that both refuses an existing name and puts whole bytes
	// under it; a filesystem without them fails the first create, saying
	// so. A name that holds anything but a regular file is NotAFile, never
	// Existing, as it is for every other call.
	CreateOnce(n Name, b []byte) (Made, error)
	// ReadAt reads up to size bytes at off, fewer at the end of the file.
	ReadAt(n Name, off, size uint64) ([]byte, error)
	// ReadAll reads the whole file, in one open to its end.
	ReadAll(n Name) ([]byte, error)
	Size(n Name) (uint64, error)
	// List is the files of the directory, sorted, and every other entry in
	// it but a create-once's temporary files, each refused; a missing
	// directory holds none.
	List() (Listing, error)
}

// Listing is a directory's entries: its files, sorted, and each entry that
// is no file of the port, refused, so an entry that should have been a
// file is said, never dropped `[LAW:no-silent-failure]`. A create-once's
// temporary files, named by the reserved prefix, are neither.
type Listing struct {
	Names   []Name
	Refused []Refused
}

// Refused is an entry of a directory that is no file of the port: its
// name as the directory holds it, and why, a NotAFile, or its path and
// NameOf's refusal of the name, ErrNotAName.
type Refused struct {
	Name string
	Why  error
}

func (r Refused) Error() string { return r.Why.Error() }

func (r Refused) Unwrap() error { return r.Why }

// Made is the outcome of a create-once: the file was created, or found
// there already.
type Made uint8

// The two answers of CreateOnce.
const (
	Existing Made = iota + 1
	Created
)

// NotFound is a file that does not exist.
type NotFound struct{ Name Name }

func (n NotFound) Error() string { return fmt.Sprintf("%s does not exist", n.Name) }

// NotAFile is a name holding something other than a regular file, a
// symlink, a FIFO or a directory among them: no call reads, appends to or
// takes it for the file it would have made.
type NotAFile struct {
	Path string
	Mode fs.FileMode
}

func (n NotAFile) Error() string {
	return fmt.Sprintf("%s is a %v, not a regular file", n.Path, n.Mode.Type())
}

// NotTheDirectory is an element of a Dir that is not a directory itself:
// a symlink, inside the base, outside it or dangling, or anything else
// that is not a directory.
type NotTheDirectory struct{ Path string }

// Changed is an entry that was another one, of the kind the step wanted,
// when it was opened than when it was looked at: it changed between the
// look and the open, so nothing is read or written through it.
type Changed struct{ Path string }

func (c Changed) Error() string {
	return fmt.Sprintf("%s changed between its look and its open, so nothing is read or written through it", c.Path)
}

func (n NotTheDirectory) Error() string {
	return fmt.Sprintf("%s is not a directory itself, so nothing is read or written through it", n.Path)
}
