// Package files is the files of one directory: appended, created once,
// read and listed. A name holds one path element, so no port reaches a
// subdirectory, and no port names another port's files by spelling
// `[LAW:decomposition]`.
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
		panic(fmt.Sprintf("files: %v", err))
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

// ErrNoDir is a Dir no constructor made: no base, or no directory in it.
var ErrNoDir = errors.New("no directory: a Dir is made by DirAt")

// Dir is a directory a Files is over: a base lit2 or git knows, trusted,
// and the elements under it, each a directory checked in turn from the
// base, so an element that is not the real directory at its name, a
// symlink among them, is refused.
type Dir struct {
	base  string
	elems []Name
}

// DirAt is the directory first in base.
func DirAt(base string, first Name) Dir { return Dir{base: base, elems: []Name{first}} }

// Sub is the directory n in d.
func (d Dir) Sub(n Name) Dir {
	return Dir{base: d.base, elems: append(slices.Clone(d.elems), n)}
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

// valid refuses a Dir no constructor made, and one with a zero element.
func (d Dir) valid() error {
	if d.base == "" || len(d.elems) == 0 {
		return fmt.Errorf("base %q, %d elements: %w", d.base, len(d.elems), ErrNoDir)
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
	Size(n Name) (uint64, error)
	// List is the files of the directory, sorted; a missing directory
	// holds none.
	List() ([]Name, error)
}

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

func (n NotTheDirectory) Error() string {
	return fmt.Sprintf("%s is not a directory itself, so nothing is read or written through it", n.Path)
}
