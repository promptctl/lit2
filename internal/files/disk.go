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

// made is what i does about the directory name in parent, at parentPath:
// nothing, to open it, and, to create it, mkdir, the directory being there
// already no failure, then an fsync of parent, so the entry survives a
// power loss before anything under it is written. The parent is fsynced
// whether this call made the entry or found it, since one found may be a
// racing writer's, not yet synced `[LAW:dataflow-not-control-flow]`.
func (i intent) made(d Dir, parent *os.Root, name, parentPath string) error {
	switch i {
	case opening:
		return nil
	case creating:
		if err := parent.Mkdir(name, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		return d.syncDir(parent, parentPath)
	}
	panic(fmt.Sprintf("files: %d is no intent", i))
}

// walk is d's directory, opened from its base, each element entered in
// turn by the step, after do has made it in its parent if do creates. The
// base is never made: it is a git dir or a work tree, git's, which exists
// before lit2 runs, and its parent is outside any root, so an entry made
// for it could not be synced. A base or an element missing to an opening,
// or a base missing to a creating, is fs.ErrNotExist.
func (d Dir) walk(do intent) (*os.Root, error) {
	if err := d.valid(); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(d.base)
	if err != nil {
		return nil, err
	}
	for i, e := range d.elems {
		var next *os.Root
		err := do.made(d, r, e.s, d.upTo(i))
		if err == nil {
			next, err = step(r, e.s, d.upTo(i+1), directory, d.seams.Gate)
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
// Make makes d where it is missing, each element inside its parent and
// fsynced into it, checked alike; its base must exist. Sync checks d alike
// and fsyncs its directory, so an entry a user made in it by its path, as
// git's repository is renamed into place, survives a power loss. All three
// are for a user handed a path, as git is, which checks the directory,
// then names it by its path.
func (d Dir) Check() error {
	return d.walked(opening, func(*os.Root) error { return nil })
}

func (d Dir) Make() error {
	return d.walked(creating, func(*os.Root) error { return nil })
}

func (d Dir) Sync() error {
	return d.walked(opening, func(r *os.Root) error { return d.syncDir(r, d.Path()) })
}

// walked is d walked as do says, then runs on its directory, and closed.
func (d Dir) walked(do intent, then func(*os.Root) error) error {
	r, err := d.walk(do)
	if err != nil {
		return err
	}
	return errors.Join(then(r), r.Close())
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

// open is n's file, opened by the step with flag in the directory, and
// the directory it is in.
func (o Disk) open(n Name, flag int) (*os.Root, *os.File, error) {
	if err := n.valid(); err != nil {
		return nil, nil, err
	}
	r, err := o.dir.walk(opening)
	if err != nil {
		return nil, nil, missing(n, err)
	}
	f, err := step(r, n.s, o.dir.PathOf(n), file(flag), o.dir.seams.Gate)
	if err != nil {
		return nil, nil, errors.Join(missing(n, err), r.Close())
	}
	return r, f, nil
}

// with runs do on n's file, opened by the step with flag in the
// directory, and closes both.
func (o Disk) with(n Name, flag int, do func(*os.File) error) error {
	r, f, err := o.open(n, flag)
	if err != nil {
		return err
	}
	return errors.Join(do(f), f.Close(), r.Close())
}

// syncDir fsyncs r's directory, at path, through the seam.
func (d Dir) syncDir(r *os.Root, path string) (err error) {
	f, err := r.Open(".")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	return d.seams.Sync(path, f)
}

func (o Disk) Append(n Name, b []byte) Appending {
	r, f, err := o.open(n, os.O_APPEND|os.O_WRONLY)
	if err != nil {
		return NotAppended{Why: err}
	}
	if _, err := f.Write(b); err != nil {
		return NotAppended{Why: errors.Join(err, f.Close(), r.Close())}
	}
	at, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		// lseek of the current offset of a descriptor this call holds open
		// fails only on a descriptor that is not one, which is a bug here
		panic(fmt.Errorf("files: the offset after an append to %s: %w", o.dir.PathOf(n), err))
	}
	// [LAW:no-silent-failure] b has landed: from here every failure, of a
	// sync or a close, leaves it there, so it is said beside the end, never
	// as an append that wrote nothing, which a caller would write again.
	// A printed echo must survive a power loss, so the append is not
	// durable until the file and the name it is under are on disk (the
	// store's decision 11).
	why := errors.Join(o.dir.seams.Sync(o.dir.PathOf(n), f), f.Close(), o.dir.syncDir(r, o.dir.Path()), r.Close())
	if why != nil {
		return AppendedNotDurable{End: uint64(at), Why: why}
	}
	return Appended{End: uint64(at)}
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

// CreateOnce takes the same steps whichever create wins: the walk, each
// directory made fsynced into its parent; the temp, written and fsynced;
// the link; the name's other temps reaped once it is placed; this call's
// own temp removed; and the directory fsynced, so a loser leaves the name
// as durable as the winner does `[LAW:dataflow-not-control-flow]`.
func (o Disk) CreateOnce(n Name, b []byte) Placing {
	if err := n.valid(); err != nil {
		return NotPlaced{Why: err}
	}
	r, err := o.dir.walk(creating)
	if err != nil {
		return NotPlaced{Why: err}
	}
	temp, err := o.temp(r, n, b)
	if err != nil {
		return NotPlaced{Why: errors.Join(err, r.Close())}
	}
	o.dir.seams.Gate(o.dir.pathIn(temp))
	made, err := o.link(r, n, temp)
	if err != nil {
		// nothing is placed, so nothing is reaped: every other temp of n
		// may still be a live create's, which may yet win
		return NotPlaced{Why: errors.Join(err, gone(o.dir.seams.Remove(r, temp)), r.Close())}
	}
	swept := o.reap(r, n, temp)
	if err := gone(o.dir.seams.Remove(r, temp)); err != nil {
		swept.Left = append(swept.Left, Leftover{Name: temp, Why: err})
	}
	// the name must survive a power loss as the appends to it will, and the
	// reaps and removals with it
	if why := errors.Join(o.dir.syncDir(r, o.dir.Path()), r.Close()); why != nil {
		return PlacedNotDurable{Made: made, Swept: swept, Why: why}
	}
	return Placed{Made: made, Swept: swept}
}

// link links temp to n in r: Created when the link made n, and Existing
// when n holds a regular file already. Two failures of the link find n
// there: the name taken, fs.ErrExist, and this call's temp gone,
// fs.ErrNotExist, reaped by a create that placed n first. Either way n is
// looked at, not followed, and a name that holds anything but a regular
// file is NotAFile; one that holds nothing is the link's failure.
func (o Disk) link(r *os.Root, n Name, temp string) (Made, error) {
	err := r.Link(temp, n.s)
	if err == nil {
		return Created, nil
	}
	if !errors.Is(err, fs.ErrExist) && !errors.Is(err, fs.ErrNotExist) {
		return 0, err
	}
	at, lerr := r.Lstat(n.s)
	if lerr != nil {
		return 0, errors.Join(err, lerr)
	}
	if refusal, refused := aFile.refusal(o.dir.PathOf(n), at.Mode()); refused {
		return 0, refusal
	}
	return Existing, nil
}

// reap removes every temp of n in r but own, once n is placed. Then each
// is a crash's leftover, the temp of a create that lost, which finds n
// there when its link fails, or a winner's whose own removal finds it
// gone; so a reap takes nothing a create still needs, and no lock orders
// them `[LAW:no-ambient-temporal-coupling]`. A temp another reap removed
// first is no one's to report.
func (o Disk) reap(r *os.Root, n Name, own string) Swept {
	s := Swept{Reaped: []string{}, Left: []Leftover{}}
	entries, err := readDir(r)
	if err != nil {
		return Swept{Reaped: []string{}, Left: []Leftover{{Name: ".", Why: fmt.Errorf("reading %s for the temps of %s: %w", o.dir.Path(), n, err)}}}
	}
	for _, e := range entries {
		if e.Name() == own || !isTempOf(e.Name(), n) {
			continue
		}
		err := o.dir.seams.Remove(r, e.Name())
		switch {
		case err == nil:
			s.Reaped = append(s.Reaped, e.Name())
		case !errors.Is(err, fs.ErrNotExist):
			s.Left = append(s.Left, Leftover{Name: e.Name(), Why: err})
		}
	}
	return s
}

// gone is err, but nil when it says the entry is not there: a temp
// another create's reap removed is removed.
func gone(err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// readDir is r's directory's entries.
func readDir(r *os.Root) (es []fs.DirEntry, err error) {
	d, err := r.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	return d.ReadDir(-1)
}

// pathIn is the path of the entry name in d, as a message names it.
func (d Dir) pathIn(name string) string { return filepath.Join(d.Path(), name) }

// temp writes b to a new temporary file in r, in the instance's mode, and
// returns its name, tempName's, which no Name can be. The bytes are on
// disk before the file is closed, so a power loss after the link leaves a
// whole file, never an empty one every append then refuses. os.Root has
// no CreateTemp, so the count runs up from 0 past every name another
// writer holds; two writers of one name each end with their own temp, and
// the link decides whose bytes are kept.
func (o Disk) temp(r *os.Root, n Name, b []byte) (string, error) {
	for i := 0; ; i++ {
		name := tempName(n, i)
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
		if err := errors.Join(werr, f.Chmod(o.mode), o.dir.seams.Sync(o.dir.pathIn(name), f), f.Close()); err != nil {
			return "", errors.Join(err, gone(o.dir.seams.Remove(r, name)))
		}
		return name, nil
	}
}

func (o Disk) List() (l Listing, err error) {
	l = Listing{Names: []Name{}, Temps: []string{}, Refused: []Refused{}}
	r, err := o.dir.walk(opening)
	if errors.Is(err, fs.ErrNotExist) {
		return l, nil // nothing is created by a read: no directory yet holds no file
	}
	if err != nil {
		return Listing{}, err
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	entries, err := readDir(r)
	if err != nil {
		return Listing{}, err
	}
	for _, e := range entries {
		if _, temp := tempOf(e.Name()); temp {
			l.Temps = append(l.Temps, e.Name())
			continue
		}
		path := o.dir.pathIn(e.Name())
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
	slices.Sort(l.Temps)
	return l, nil
}

func (o Disk) Temps(n Name) (ts []string, err error) {
	ts = []string{}
	if err := n.valid(); err != nil {
		return nil, err
	}
	r, err := o.dir.walk(opening)
	if errors.Is(err, fs.ErrNotExist) {
		return ts, nil // nothing is created by a read: no directory yet holds no temp
	}
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	entries, err := readDir(r)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if isTempOf(e.Name(), n) {
			ts = append(ts, e.Name())
		}
	}
	slices.Sort(ts)
	return ts, nil
}
