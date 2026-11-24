package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
)

// Disk is Files on disk over one directory, every file it makes in one
// mode. Each call opens the directory's base as an os.Root, enters each of
// its elements by the step, then opens a file by the same step: it is the
// one enforcer of what an entry may be on disk `[LAW:single-enforcer]`.
type Disk struct {
	dir  Dir
	mode fs.FileMode
}

// readable is the mode At makes files in, 0644, readable by anyone who
// can read the directory.
const readable fs.FileMode = 0o644

// At is the files of d, which need not exist yet, every file made
// readable.
func At(d Dir) Disk { return AtMode(d, readable) }

// AtMode is the files of d, each made in mode: a secret's files are 0600
// through this same port.
func AtMode(d Dir, mode fs.FileMode) Disk { return Disk{dir: d, mode: mode} }

// intent is what walking a Dir does about each directory on its way: an
// opening finds it there, and a creating makes it where it is missing.
type intent uint8

const (
	opening intent = iota + 1
	creating
)

// made is what i does about the directory mkdir would make: nothing, to
// open it, and, to create it, mkdir, the directory being there already
// no failure.
func (i intent) made(mkdir func() error) error {
	switch i {
	case opening:
		return nil
	case creating:
		if err := mkdir(); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		return nil
	}
	panic(fmt.Sprintf("files: %d is no intent", i))
}

// walk is d's directory, opened from its base, each element entered in
// turn by the step, after do has made it in its parent if do creates. The
// base is made by os.MkdirAll, as the git dirs and the work tree a base
// names are lit2's or git's own. An element missing to an opening is
// fs.ErrNotExist.
func (d Dir) walk(do intent) (*os.Root, error) {
	if err := d.valid(); err != nil {
		return nil, err
	}
	if err := do.made(func() error { return os.MkdirAll(d.base, 0o755) }); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(d.base)
	if err != nil {
		return nil, err
	}
	for i, e := range d.elems {
		var next *os.Root
		err := do.made(func() error { return r.Mkdir(e.s, 0o755) })
		if err == nil {
			next, err = step(r, e.s, d.upTo(i+1), directory, d.gate)
		}
		if err = errors.Join(err, r.Close()); err != nil {
			if next != nil {
				err = errors.Join(err, next.Close())
			}
			return nil, err
		}
		r = next
	}
	return r, nil
}

// Check checks each element of d by the step every Files call takes,
// holding nothing open, and makes nothing: one missing is fs.ErrNotExist.
// Make makes d where it is missing, its base by os.MkdirAll and each
// element inside its parent, checked alike. Both are for a user handed a
// path, as git is, which checks the directory, then names it by its path.
func (d Dir) Check() error { return d.walked(opening) }

func (d Dir) Make() error { return d.walked(creating) }

// walked is d walked as do says, and closed.
func (d Dir) walked(do intent) error {
	r, err := d.walk(do)
	if err != nil {
		return err
	}
	return r.Close()
}

// kind is a kind of entry the step reaches: the type its mode must be, and
// its refusal, an R, of an entry that is not one.
type kind[R error] struct {
	is     func(fs.FileMode) bool
	refuse func(path string, m fs.FileMode) R
}

// The two kinds of entry: a directory, and a regular file.
var (
	aDirectory = kind[NotTheDirectory]{
		is:     fs.FileMode.IsDir,
		refuse: func(path string, _ fs.FileMode) NotTheDirectory { return NotTheDirectory{Path: path} },
	}
	aFile = kind[NotAFile]{
		is:     fs.FileMode.IsRegular,
		refuse: func(path string, m fs.FileMode) NotAFile { return NotAFile{Path: path, Mode: m} },
	}
)

// refusal is k's refusal of the entry at path, of mode m, and whether k
// refuses it.
func (k kind[R]) refusal(path string, m fs.FileMode) (R, bool) {
	return k.refuse(path, m), !k.is(m)
}

// entry is how the step opens one kind of entry, a T, and sees what it
// opened.
type entry[T io.Closer, R error] struct {
	kind[R]
	open func(parent *os.Root, name string) (T, error)
	stat func(T) (fs.FileInfo, error)
}

// directory is a directory, entered as an os.Root.
var directory = entry[*os.Root, NotTheDirectory]{
	kind: aDirectory,
	open: (*os.Root).OpenRoot,
	stat: func(r *os.Root) (fs.FileInfo, error) { return r.Stat(".") },
}

// file is a regular file, opened with flag and O_NONBLOCK, so that no open
// of a FIFO swapped in after the look waits on its other end.
func file(flag int) entry[*os.File, NotAFile] {
	return entry[*os.File, NotAFile]{
		kind: aFile,
		open: func(parent *os.Root, name string) (*os.File, error) {
			return parent.OpenFile(name, flag|syscall.O_NONBLOCK, 0)
		},
		stat: (*os.File).Stat,
	}
}

// step is the one way an entry of a Dir is reached, a directory entered
// or a file opened: the entry name in parent, at path, looked at without
// following it and refused unless it is k's kind, so a symlink, wherever
// it points, or anything else is never opened; then opened, and what was
// opened refused: by k's refusal when it is not k's kind, and as Changed
// when it is but is not the very entry looked at. The check of the opened
// kind holds where a swap reuses the looked-at entry's inode number, which
// SameFile alone would take for the same entry `[LAW:single-enforcer]`.
// gate runs between the look and the open.
func step[T io.Closer, R error](parent *os.Root, name, path string, k entry[T, R], gate func(path string)) (T, error) {
	var none T
	at, err := parent.Lstat(name)
	if err != nil {
		return none, err
	}
	if r, refused := k.refusal(path, at.Mode()); refused {
		return none, r
	}
	gate(path)
	h, err := k.open(parent, name)
	if err != nil {
		return none, err
	}
	opened, err := k.stat(h)
	if err != nil {
		return none, errors.Join(err, h.Close())
	}
	if r, refused := k.refusal(path, opened.Mode()); refused {
		return none, errors.Join(r, h.Close())
	}
	if !os.SameFile(opened, at) {
		return none, errors.Join(Changed{Path: path}, h.Close())
	}
	return h, nil
}

// missing is err as NotFound when it says the file, or a directory on its
// way, does not exist.
func missing(n Name, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return NotFound{Name: n}
	}
	return err
}

// with runs do on n's file, opened by the step with flag in the
// directory, and closes both.
func (o Disk) with(n Name, flag int, do func(*os.File) error) (err error) {
	if err := n.valid(); err != nil {
		return err
	}
	r, err := o.dir.walk(opening)
	if err != nil {
		return missing(n, err)
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	f, err := step(r, n.s, o.dir.PathOf(n), file(flag), o.dir.gate)
	if err != nil {
		return missing(n, err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	return do(f)
}

func (o Disk) Append(n Name, b []byte) (end uint64, err error) {
	err = o.with(n, os.O_APPEND|os.O_WRONLY, func(f *os.File) error {
		if _, err := f.Write(b); err != nil {
			return err
		}
		// a printed echo must survive a power loss, so the append is not
		// done until it is on disk (the store's decision 11)
		if err := f.Sync(); err != nil {
			return err
		}
		at, err := f.Seek(0, io.SeekCurrent)
		end = uint64(at)
		return err
	})
	return end, err
}

func (o Disk) ReadAt(n Name, off, size uint64) (b []byte, err error) {
	err = o.with(n, os.O_RDONLY, func(f *os.File) error {
		// no offset or size past math.MaxInt64, which a section reader
		// cannot hold, names a byte of a file
		b, err = io.ReadAll(io.NewSectionReader(f, int64(min(off, math.MaxInt64)), int64(min(size, math.MaxInt64))))
		return err
	})
	return b, err
}

func (o Disk) ReadAll(n Name) (b []byte, err error) {
	err = o.with(n, os.O_RDONLY, func(f *os.File) error {
		b, err = io.ReadAll(f)
		return err
	})
	return b, err
}

func (o Disk) Size(n Name) (size uint64, err error) {
	err = o.with(n, os.O_RDONLY, func(f *os.File) error {
		info, err := f.Stat()
		if err == nil {
			size = uint64(info.Size())
		}
		return err
	})
	return size, err
}

func (o Disk) CreateOnce(n Name, b []byte) (made Made, err error) {
	if err := n.valid(); err != nil {
		return 0, err
	}
	r, err := o.dir.walk(creating)
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	temp, err := o.temp(r, n, b)
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, r.Remove(temp)) }()
	err = r.Link(temp, n.s)
	if errors.Is(err, fs.ErrExist) {
		at, err := r.Lstat(n.s)
		if err != nil {
			return 0, err
		}
		if r, refused := aFile.refusal(o.dir.PathOf(n), at.Mode()); refused {
			return 0, r
		}
		return Existing, nil
	}
	if err != nil {
		return 0, err
	}
	// the new name must survive a power loss as the appends to it will
	return Created, syncDir(r)
}

func syncDir(r *os.Root) (err error) {
	d, err := r.Open(".")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	return d.Sync()
}

// temp writes b to a new temporary file in r, in the instance's mode, and
// returns its name, tempPrefix, n and a count, which no Name can be and
// List never reports. The bytes are on disk before the file is closed, so
// a power loss after the link leaves a whole file, never an empty one
// every append then refuses. os.Root has no CreateTemp, so the count runs
// up from 0 past every name another writer holds; two writers of one
// name each end with their own temp, and the link decides whose bytes
// are kept.
func (o Disk) temp(r *os.Root, n Name, b []byte) (string, error) {
	for i := 0; ; i++ {
		name := fmt.Sprintf("%s%s-%d", tempPrefix, n.s, i)
		f, err := r.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, o.mode)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(b)
		// the mode is the instance's whatever the umask, so a secret's file
		// is 0600 and a readable one 0644
		if err := errors.Join(werr, f.Chmod(o.mode), f.Sync(), f.Close()); err != nil {
			return "", errors.Join(err, r.Remove(name))
		}
		return name, nil
	}
}

func (o Disk) List() (l Listing, err error) {
	l = Listing{Names: []Name{}, Refused: []Refused{}}
	r, err := o.dir.walk(opening)
	if errors.Is(err, fs.ErrNotExist) {
		return l, nil // nothing is created by a read: no directory yet holds no file
	}
	if err != nil {
		return Listing{}, err
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	d, err := r.Open(".")
	if err != nil {
		return Listing{}, err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	entries, err := d.ReadDir(-1)
	if err != nil {
		return Listing{}, err
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), tempPrefix) {
			continue // a create-once's temporary file, which no Name can be
		}
		path := filepath.Join(o.dir.Path(), e.Name())
		n, err := NameOf(e.Name())
		if err != nil {
			l.Refused = append(l.Refused, Refused{Name: e.Name(), Why: fmt.Errorf("%s: %w", path, err)})
			continue
		}
		// the entry's type as the directory holds it, not following a link
		if r, refused := aFile.refusal(path, e.Type()); refused {
			l.Refused = append(l.Refused, Refused{Name: e.Name(), Why: r})
			continue
		}
		l.Names = append(l.Names, n)
	}
	slices.SortFunc(l.Names, func(a, b Name) int { return strings.Compare(a.s, b.s) })
	return l, nil
}
