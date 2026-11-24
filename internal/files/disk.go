package files

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Disk is Files on disk over one directory, every file it makes in one
// mode. Each call opens the directory inside its base as an os.Root, the
// one enforcer of containment `[LAW:single-enforcer]`: a name, or a
// symlink a name holds, that leaves the directory is refused by the open
// itself.
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

// open is the directory, opened inside its base, made first, with its
// base, when create says so. A directory that does not exist is
// fs.ErrNotExist. One whose name in the base is not the directory itself,
// a symlink to one even inside the base, is NotTheDirectory: the opened
// directory and what the base holds at its name must be one file.
func (o Disk) open(create bool) (*os.Root, error) {
	if create {
		if err := os.MkdirAll(o.dir.base, 0o755); err != nil {
			return nil, err
		}
	}
	base, err := os.OpenRoot(o.dir.base)
	if err != nil {
		return nil, err
	}
	defer func() { _ = base.Close() }() // read-only; the directory opened from it outlives it
	if create {
		if err := base.Mkdir(o.dir.name.s, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
	}
	at, err := base.Lstat(o.dir.name.s)
	if err != nil {
		return nil, err
	}
	r, err := base.OpenRoot(o.dir.name.s)
	if err != nil {
		return nil, err
	}
	opened, err := r.Stat(".")
	if err != nil {
		return nil, errors.Join(err, r.Close())
	}
	if !os.SameFile(opened, at) {
		return nil, errors.Join(NotTheDirectory{Path: o.dir.Path()}, r.Close())
	}
	return r, nil
}

// missing is err as NotFound when it says the file, or its directory,
// does not exist.
func missing(n Name, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return NotFound{Name: n}
	}
	return err
}

func (o Disk) Append(n Name, b []byte) (end uint64, err error) {
	if err := n.valid(); err != nil {
		return 0, err
	}
	r, err := o.open(false)
	if err != nil {
		return 0, missing(n, err)
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	f, err := r.OpenFile(n.s, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return 0, missing(n, err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	if _, err := f.Write(b); err != nil {
		return 0, err
	}
	// a printed echo must survive a power loss, so the append is not done
	// until it is on disk (the store's decision 11)
	if err := f.Sync(); err != nil {
		return 0, err
	}
	at, err := f.Seek(0, io.SeekCurrent)
	return uint64(at), err
}

func (o Disk) CreateOnce(n Name, b []byte) (made Made, err error) {
	if err := n.valid(); err != nil {
		return 0, err
	}
	r, err := o.open(true)
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
		return o.existing(r, n)
	}
	if err != nil {
		return 0, err
	}
	// the new name must survive a power loss as the appends to it will
	return Created, syncDir(r)
}

// existing is the answer of a create-once whose name is taken: Existing
// when it holds a regular file, NotAFile when it holds anything else, a
// symlink among them, read without following it.
func (o Disk) existing(r *os.Root, n Name) (Made, error) {
	info, err := r.Lstat(n.s)
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() {
		return 0, NotAFile{Path: filepath.Join(o.dir.Path(), n.s), Mode: info.Mode()}
	}
	return Existing, nil
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
// returns its name, .tmp-<name>-<i>, which List never reports. The bytes are
// on disk before the file is closed, so a power loss after the link leaves
// a whole file, never an empty one every append then refuses. os.Root has
// no CreateTemp, so i counts up from 0 past every name another writer
// holds; two writers of one name each end with their own temp, and the
// link decides whose bytes are kept.
func (o Disk) temp(r *os.Root, n Name, b []byte) (string, error) {
	for i := 0; ; i++ {
		name := fmt.Sprintf(".tmp-%s-%d", n.s, i)
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

func (o Disk) ReadAt(n Name, off, size uint64) (b []byte, err error) {
	if err := n.valid(); err != nil {
		return nil, err
	}
	r, err := o.open(false)
	if err != nil {
		return nil, missing(n, err)
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	f, err := r.Open(n.s)
	if err != nil {
		return nil, missing(n, err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	return io.ReadAll(io.NewSectionReader(f, int64(off), int64(size)))
}

func (o Disk) Size(n Name) (uint64, error) {
	info, err := o.stat(n)
	if err != nil {
		return 0, err
	}
	return uint64(info.Size()), nil
}

// Mode is the permission bits of the file n.
func (o Disk) Mode(n Name) (fs.FileMode, error) {
	info, err := o.stat(n)
	if err != nil {
		return 0, err
	}
	return info.Mode().Perm(), nil
}

func (o Disk) stat(n Name) (info fs.FileInfo, err error) {
	if err := n.valid(); err != nil {
		return nil, err
	}
	r, err := o.open(false)
	if err != nil {
		return nil, missing(n, err)
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	info, err = r.Stat(n.s)
	if err != nil {
		return nil, missing(n, err)
	}
	return info, nil
}

func (o Disk) List() (ns []Name, err error) {
	r, err := o.open(false)
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
		// a name starting with . is a create-once's temporary file
		if e.Type().IsRegular() && !strings.HasPrefix(e.Name(), ".") {
			ns = append(ns, Name{s: e.Name()})
		}
	}
	slices.SortFunc(ns, func(a, b Name) int { return strings.Compare(a.s, b.s) })
	return ns, nil
}
