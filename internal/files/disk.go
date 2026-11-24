package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"slices"
	"strings"
	"syscall"
)

// Disk is Files on disk over one directory, every file it makes in one
// mode. Each call opens the directory's base as an os.Root and walks its
// elements by the directory step, then reaches a file by the file step:
// those two steps are the one enforcer of what a name may be on disk
// `[LAW:single-enforcer]`.
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

// open is d's directory, each element entered by the directory step from
// its base. An element that does not exist is fs.ErrNotExist.
func (d Dir) open() (*os.Root, error) {
	return d.walk(func(*os.Root, Name) error { return nil })
}

// create is d's directory, its base made when missing, as the git dirs
// and the work tree a base names are lit2's or git's own, and each
// missing element made inside its parent before it is entered.
func (d Dir) create() (*os.Root, error) {
	if err := d.valid(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(d.base, 0o755); err != nil {
		return nil, err
	}
	return d.walk(func(parent *os.Root, e Name) error {
		if err := parent.Mkdir(e.s, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		return nil
	})
}

// walk opens d's base and enters each element in turn, after before has
// prepared it in its parent.
func (d Dir) walk(before func(parent *os.Root, e Name) error) (*os.Root, error) {
	if err := d.valid(); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(d.base)
	if err != nil {
		return nil, err
	}
	for i, e := range d.elems {
		next, err := d.enter(r, e, i, before)
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

// enter is the directory step: element i of d, e, in parent, entered only
// when it is a directory itself, and the directory opened is the one seen.
// A symlink, dangling or not, or anything else not a directory is
// NotTheDirectory.
func (d Dir) enter(parent *os.Root, e Name, i int, before func(*os.Root, Name) error) (*os.Root, error) {
	if err := before(parent, e); err != nil {
		return nil, err
	}
	at, err := parent.Lstat(e.s)
	if err != nil {
		return nil, err
	}
	refused := NotTheDirectory{Path: d.upTo(i + 1)}
	if !at.IsDir() {
		return nil, refused
	}
	r, err := parent.OpenRoot(e.s)
	if err != nil {
		return nil, err
	}
	opened, err := r.Stat(".")
	if err != nil {
		return nil, errors.Join(err, r.Close())
	}
	if !os.SameFile(opened, at) {
		return nil, errors.Join(refused, r.Close())
	}
	return r, nil
}

// file is the file step: n in r opened with flag, only when it is a
// regular file and the file opened is the one seen. O_NONBLOCK is added
// so that no open, of a FIFO swapped in after the look, waits. Anything
// but a regular file is NotAFile; a missing one is NotFound.
func (o Disk) file(r *os.Root, n Name, flag int) (*os.File, error) {
	at, err := r.Lstat(n.s)
	if err != nil {
		return nil, missing(n, err)
	}
	if err := o.regular(n, at); err != nil {
		return nil, err
	}
	f, err := r.OpenFile(n.s, flag|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, missing(n, err)
	}
	opened, err := f.Stat()
	if err != nil {
		return nil, errors.Join(err, f.Close())
	}
	if !os.SameFile(opened, at) {
		return nil, errors.Join(NotAFile{Path: o.dir.PathOf(n), Mode: opened.Mode()}, f.Close())
	}
	return f, nil
}

// regular is nil for a regular file at n, seen without following it, and
// NotAFile for anything else.
func (o Disk) regular(n Name, at fs.FileInfo) error {
	if !at.Mode().IsRegular() {
		return NotAFile{Path: o.dir.PathOf(n), Mode: at.Mode()}
	}
	return nil
}

// missing is err as NotFound when it says the file, or a directory on its
// way, does not exist.
func missing(n Name, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return NotFound{Name: n}
	}
	return err
}

// with runs do on n's file, opened by the file step with flag in the
// directory, and closes both.
func (o Disk) with(n Name, flag int, do func(*os.File) error) (err error) {
	if err := n.valid(); err != nil {
		return err
	}
	r, err := o.dir.open()
	if err != nil {
		return missing(n, err)
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	f, err := o.file(r, n, flag)
	if err != nil {
		return err
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
		// a section past math.MaxInt64 ends there, so any size reads to
		// the end of the file
		b, err = io.ReadAll(io.NewSectionReader(f, int64(min(off, math.MaxInt64)), int64(min(size, math.MaxInt64))))
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
	r, err := o.dir.create()
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
		if err := o.regular(n, at); err != nil {
			return 0, err
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

func (o Disk) List() (ns []Name, err error) {
	r, err := o.dir.open()
	if errors.Is(err, fs.ErrNotExist) {
		return []Name{}, nil // nothing is created by a read: no directory yet holds no file
	}
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	d, err := r.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	entries, err := d.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	ns = []Name{}
	for _, e := range entries {
		if e.Type().IsRegular() && !strings.HasPrefix(e.Name(), tempPrefix) {
			ns = append(ns, Name{s: e.Name()})
		}
	}
	slices.SortFunc(ns, func(a, b Name) int { return strings.Compare(a.s, b.s) })
	return ns, nil
}
