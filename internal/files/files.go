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
	"strings"
)

// Name is a file's name in its directory: one path element, never empty,
// . or .., and holding no separator or NUL. Only NameOf, MustName and a
// listing make one `[LAW:parse-dont-validate]`.
type Name struct{ s string }

// ErrNotAName is text that is no one file's name in a directory.
var ErrNotAName = errors.New("not a file's name in one directory")

// NameOf is s as a name, refused with ErrNotAName when it is empty, . or
// .., or holds /, \ or NUL, any of which would reach outside the directory
// or name none of its files.
func NameOf(s string) (Name, error) {
	if s == "" || s == "." || s == ".." || strings.ContainsAny(s, "/\\\x00") {
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

// Dir is a directory a Files is over: the directory name in base. base is
// a directory lit2 or git knows, and the port opens name inside it, so a
// name that is not the real directory there is refused.
type Dir struct {
	base string
	name Name
}

// DirAt is the directory name in base.
func DirAt(base string, name Name) Dir { return Dir{base: base, name: name} }

// Path is the directory's path, as a message names it.
func (d Dir) Path() string { return filepath.Join(d.base, d.name.s) }

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
	// Existing.
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

// NotAFile is a name CreateOnce found holding something other than a
// regular file, a symlink or a directory, which it never takes for the
// file it would have made.
type NotAFile struct {
	Path string
	Mode fs.FileMode
}

func (n NotAFile) Error() string {
	return fmt.Sprintf("%s is a %v, not a regular file", n.Path, n.Mode.Type())
}

// NotTheDirectory is a directory whose name in its base is not the
// directory itself: a symlink to one, inside the base or outside it.
type NotTheDirectory struct{ Path string }

func (n NotTheDirectory) Error() string {
	return fmt.Sprintf("%s is not a directory itself, so nothing is read or written through it", n.Path)
}
