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
	"strconv"
	"strings"

	"github.com/promptctl/lit2/internal/files/internal/seam"
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

// tempName is the i'th temporary file of a create-once of n: the one
// spelling of a temp, which tempOf reads back `[LAW:one-source-of-truth]`.
func tempName(n Name, i int) string { return tempPrefix + n.s + "-" + strconv.Itoa(i) }

// tempOf is the name whose temporary file entry is, when it is one: the
// prefix, a name, a dash and a count of digits alone. So .tmp-a-1-0 is a
// temp of a-1, never of a, and .tmp-x is no temp at all.
func tempOf(entry string) (Name, bool) {
	rest, ok := strings.CutPrefix(entry, tempPrefix)
	dash := strings.LastIndexByte(rest, '-')
	if !ok || dash < 0 || dash == len(rest)-1 || strings.Trim(rest[dash+1:], "0123456789") != "" {
		return Name{}, false
	}
	n, err := NameOf(rest[:dash])
	return n, err == nil
}

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
	// seams are the steps a test stands in for: the OS's own, but in a
	// Dir a test built through filestest, which gates a swap of an entry or
	// a race between a temp and its link, or sees or fails an fsync
	seams seam.Seams
}

// DirAt is the directory first in base, which must be absolute; a base
// that is not is a bug, so it panics `[LAW:parse-dont-validate]`.
func DirAt(base string, first Name) Dir {
	if !filepath.IsAbs(base) {
		panic(fmt.Errorf("files: DirAt(%q): %w", base, ErrNotABase))
	}
	return Dir{base: base, elems: []Name{first}, seams: seam.OS()}
}

// Seamed is d with s at its seams: a test's Dir, whose fsyncs, removals
// and steps it sees, fails or gates. Only files and filestest can build a
// Seams, since its package is internal to files, so a test gets one from
// filestest `[LAW:locality-or-seam]`.
func (d Dir) Seamed(s seam.Seams) Dir {
	d.seams = s
	return d
}

// Sub is the directory n in d.
func (d Dir) Sub(n Name) Dir {
	return Dir{base: d.base, elems: append(slices.Clone(d.elems), n), seams: d.seams}
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
	return Dir{base: d.base, elems: slices.Clone(d.elems[:len(d.elems)-1]), seams: d.seams}
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
	// descriptor, so concurrent appends land whole with no lock, then
	// fsyncs the file. The file's name is made durable by the step that
	// establishes it, its create or a Sync, never by each append. A
	// missing file is NotAppended, NotFound.
	Append(n Name, b []byte) Appending
	// CreateOnce creates the file holding b unless it exists: b is written
	// to a temporary file, fsynced and linked into place, so exactly one
	// creation wins and the file is never seen, nor left by a crash,
	// partly written. Each directory it makes on the way is fsynced into
	// its parent before anything under it is written, and the directory
	// is fsynced after the link whether this call won or lost, so a loser
	// is as durable as the winner; a sync that fails on the way stops
	// nothing, and the file placed or found is PlacedNotDurable. Once the
	// name holds the file, the other
	// temporary files of the name are reaped: each is a crash's leftover,
	// or the temp of a create that has lost. On disk this needs hard
	// links, since link(2) is the one call that both refuses an existing
	// name and puts whole bytes under it; a filesystem without them fails
	// the first create, saying so. A name that holds anything but a
	// regular file is NotPlaced, NotAFile, never Existing, as it is for
	// every other call.
	CreateOnce(n Name, b []byte) Placing
	// Sync fsyncs the directory, so a name in it that another call
	// created, and may not have synced yet, survives a power loss. Once a
	// sync of the directory has failed, through any port of the process,
	// every later one answers that failure,
	// since the failed one may have dropped what it held and a second may
	// pass over it; that is what one process knows, and a later run that
	// syncs again may pass, so the run that saw the failure is the run
	// that says it.
	Sync() error
	// Dir is the directory the port is of, which names its files.
	Dir() Dir
	// ReadAt reads up to size bytes at off, fewer at the end of the file.
	ReadAt(n Name, off, size uint64) ([]byte, error)
	// ReadAll reads the whole file, in one open to its end.
	ReadAll(n Name) ([]byte, error)
	Size(n Name) (uint64, error)
	// List is the files of the directory, sorted, the temporary files of
	// create-onces, what a create that never ran again left
	// `[LAW:no-silent-failure]`, and every other entry in it, each
	// refused; a missing directory holds none.
	List() (Listing, error)
}

// Appending is what an append did: wrote nothing whole, or landed b
// whole, durable or not. Its three states are its three variants, so no
// caller reads an end without saying which `[LAW:types-are-the-program]`.
//
//sumtype:decl
type Appending interface{ appending() }

// NotAppended is an append that wrote no whole b: a short write leaves a
// tail no reader takes for a frame, so writing b again is safe.
type NotAppended struct{ Why error }

// Appended is b landed whole at the end, the file ending at End, and the
// file fsynced.
type Appended struct{ End uint64 }

// AppendedNotDurable is b landed whole, the file ending at End, and a
// sync on its way failed: b is there to every read, and may not be after
// a power loss. No later sync of the file in the process is trusted over
// it, since one
// that failed may have dropped the bytes it held and a second may pass
// over them.
type AppendedNotDurable struct {
	Appended
	Why error
}

func (NotAppended) appending()        {}
func (Appended) appending()           {}
func (AppendedNotDurable) appending() {}

// Placing is what a create-once did: placed no file at the name, or
// placed or found one, durable or not, and what it did about the name's
// other temporary files `[LAW:types-are-the-program]`.
//
//sumtype:decl
type Placing interface{ placing() }

// NotPlaced is a create-once that linked no file at the name and found
// none it may take: nothing of it is at the name, and it reaped nothing.
// Left is its own temporary file, when it could not remove it.
type NotPlaced struct {
	Why  error
	Left []Leftover
}

// Placed is the name holding a regular file, which this call created or
// found, with every sync on its way passed, and the reap of the name's
// other temporary files.
type Placed struct {
	Made Made
	Reap Reap
}

// PlacedNotDurable is a Placed whose sync on its way failed: the file is
// there to every read, and the name may not be after a power loss.
type PlacedNotDurable struct {
	Placed
	Why error
}

func (NotPlaced) placing()        {}
func (Placed) placing()           {}
func (PlacedNotDurable) placing() {}

// Reap is what a create-once that placed its name did about the name's
// temporary files: the others it removed, a crash's leftovers or the
// temps of creates that lost; each it could not remove, its own among
// them; and why the directory could not be listed for them, nil when it
// was. Nothing it leaves is silent `[LAW:no-silent-failure]`. The zero
// Reap is a reap that found nothing to remove.
type Reap struct {
	Reaped   []string
	Left     []Leftover
	Unlisted error
}

// Leftover is a temporary file a create-once could not remove: its name
// in the directory, and why.
type Leftover struct {
	Name string
	Why  error
}

// Listing is a directory's entries: its files, sorted; the temporary
// files of create-onces, sorted, which a create of their name reaps; and
// each entry that is no file of the port, refused, so an entry that should
// have been a file is said, never dropped `[LAW:no-silent-failure]`.
type Listing struct {
	Names   []Name
	Temps   []string
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

// Made is the outcome of a create-once that placed its name: the file was
// created, or found there already.
type Made uint8

// The two answers of a placing CreateOnce.
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
