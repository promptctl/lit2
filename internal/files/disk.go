package files

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
)

// Disk is Files on disk over one directory, every file it makes in one
// mode. Each call opens the directory's base as an os.Root, enters each of
// its elements by the step, then opens a file by the same step: it is the
// one enforcer of what an entry may be on disk `[LAW:single-enforcer]`.
type Disk struct {
	dir  Dir
	mode fs.FileMode
	// durability is the port's syncs, shared by every copy of the port:
	// once one fails, every later one answers it
	durability *Durability
}

// readable is the mode At makes files in, 0644, readable by anyone who
// can read the directory.
const readable fs.FileMode = 0o644

// At is the files of d, which need not exist yet, every file made
// readable.
func At(d Dir) Disk { return AtMode(d, readable) }

// AtMode is the files of d, each made in mode: a secret's files are 0600
// through this same port.
func AtMode(d Dir, mode fs.FileMode) Disk {
	return Disk{dir: d, mode: mode, durability: NewDurability()}
}

// Durability is what one process knows of the syncs a writer's files
// rely on: that none has failed, or the first that did, which every later
// sync answers without syncing, since a sync that failed may have dropped
// what it held and a second may pass over it. It is the one keeper of
// that rule `[LAW:single-enforcer]`: a Disk holds one for the syncs of its
// port, and lit's repository one for its entry. Its state is shared by
// the pointer, never by a Dir, which is a value
// `[LAW:no-shared-mutable-globals]`. It knows nothing across processes: a
// later run whose sync passes cannot know an earlier one failed, so the
// run that saw the failure is the run that says it.
type Durability struct{ failed atomic.Pointer[error] }

// NewDurability is the syncs of a writer none of whose syncs has failed.
func NewDurability() *Durability { return &Durability{} }

// sync runs do, one fsync, unless a sync failed before, and keeps the
// first failure for every later sync to answer.
func (u *Durability) sync(do func() error) error {
	if failed := u.failed.Load(); failed != nil {
		return *failed
	}
	if err := do(); err != nil {
		u.failed.CompareAndSwap(nil, &err)
		return *u.failed.Load()
	}
	return nil
}

// syncDir fsyncs r's directory, d's at path, through the seam.
func (u *Durability) syncDir(d Dir, r *os.Root, path string) (err error) {
	f, err := r.Open(".")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	return u.sync(func() error { return d.seams.Sync(path, f) })
}

// walk is d's directory, opened from its base, each element entered in
// turn by the step. A base or an element missing is fs.ErrNotExist.
func (d Dir) walk() (*os.Root, error) {
	return d.enter(func(*os.Root, string, string) error { return nil })
}

// walking is what walking a Dir to create in it did: failed, or reached
// its directory, every entry on the way synced into its parent or not.
//
//sumtype:decl
type walking interface{ walking() }

type (
	walkFailed     struct{ why error }
	walked         struct{ r *os.Root }
	walkedUnsynced struct {
		r   *os.Root
		why error
	}
)

func (walkFailed) walking()     {}
func (walked) walking()         {}
func (walkedUnsynced) walking() {}

// walkMaking is d's directory as walk reaches it, each element first made
// in its parent where it is missing, the directory being there already no
// failure, then its parent fsynced through u, so the entry survives a
// power loss. The parent is fsynced whether this call made the entry or
// found it, since one found may be a racing writer's, not yet synced
// `[LAW:dataflow-not-control-flow]`. A failed mkdir stops the walk; a
// failed sync does not, since the entry is there to every later step:
// the walk goes on and says it. The base is never made: it is a git dir
// or a work tree, git's, which exists before lit2 runs, and its parent is
// outside any root, so an entry made for it could not be synced.
func (d Dir) walkMaking(u *Durability) walking {
	var unsynced error
	r, err := d.enter(func(parent *os.Root, name, parentPath string) error {
		if err := parent.Mkdir(name, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		if err := u.syncDir(d, parent, parentPath); err != nil && unsynced == nil {
			unsynced = err
		}
		return nil
	})
	switch {
	case err != nil:
		return walkFailed{why: err}
	case unsynced != nil:
		return walkedUnsynced{r: r, why: unsynced}
	}
	return walked{r: r}
}

// enter is d's directory, opened from its base, each element entered in
// turn by the step once made has run on it in its parent, at parentPath.
func (d Dir) enter(made func(parent *os.Root, name, parentPath string) error) (*os.Root, error) {
	if err := d.valid(); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(d.base)
	if err != nil {
		return nil, err
	}
	for i, e := range d.elems {
		var next *os.Root
		err := made(r, e.s, d.upTo(i))
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
// fsynced into it through u, checked alike; its base must exist, and a
// failed sync is its failure. Sync checks d alike and fsyncs its
// directory through u, so an entry a user made in it by its path, as
// git's repository is renamed into place, survives a power loss. All
// three are for a user handed a path, as git is, which checks the
// directory, then names it by its path.
func (d Dir) Check() error {
	r, err := d.walk()
	if err != nil {
		return err
	}
	return r.Close()
}

func (u *Durability) Make(d Dir) error {
	walk := d.walkMaking(u)
	switch w := walk.(type) {
	case walkFailed:
		return w.why
	case walked:
		return w.r.Close()
	case walkedUnsynced:
		return errors.Join(w.why, w.r.Close())
	}
	panic(fmt.Sprintf("files: %T is no walking", walk))
}

func (u *Durability) Sync(d Dir) error {
	r, err := d.walk()
	if err != nil {
		return err
	}
	return errors.Join(u.syncDir(d, r, d.Path()), r.Close())
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
	r, err := o.dir.walk()
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
	// The name it is under was made durable by the step that established
	// it, the create or a Sync, so the append fsyncs the file alone.
	landed := Appended{End: uint64(at)}
	synced := o.durability.sync(func() error { return o.dir.seams.Sync(o.dir.PathOf(n), f) })
	if why := errors.Join(synced, f.Close(), r.Close()); why != nil {
		return AppendedNotDurable{Appended: landed, Why: why}
	}
	return landed
}

func (o Disk) Sync() error { return o.durability.Sync(o.dir) }

func (o Disk) Dir() Dir { return o.dir }

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
	var r *os.Root
	// a sync on the way that failed, which stops no create
	var unsynced error
	switch w := o.dir.walkMaking(o.durability).(type) {
	case walkFailed:
		return NotPlaced{Why: w.why}
	case walked:
		r = w.r
	case walkedUnsynced:
		r, unsynced = w.r, w.why
	}
	var temp string
	switch t := o.temp(r, n, b).(type) {
	case tempUnwritten:
		return NotPlaced{Why: errors.Join(t.why, r.Close()), Left: t.left}
	case tempWritten:
		temp = t.name
	}
	o.dir.seams.Gate(o.dir.pathIn(temp))
	// nothing is placed until the name is looked at, so a NotPlaced
	// reaps nothing: every other temp of n may still be a live create's,
	// which may yet win
	unplaced := func(why error) Placing {
		// the temp is removed before the root it is removed through closes
		left := o.dir.remove(r, temp).left(temp)
		return NotPlaced{Why: errors.Join(why, r.Close()), Left: left}
	}
	var placed Placed
	var own removal
	switch l := link(r, temp, n).(type) {
	case unlinked:
		return unplaced(l.why)
	case linked:
		placed.Made, own = Created, o.dir.remove(r, temp)
	case taken:
		// a temp a winner's reap removed first is already gone to the
		// removal, which says so
		if err := o.found(r, n); err != nil {
			return unplaced(errors.Join(l.why, err))
		}
		placed.Made, own = Existing, o.dir.remove(r, temp)
	}
	placed.Reap = o.reap(r, n, temp)
	placed.Reap.Left = append(placed.Reap.Left, own.left(temp)...)
	// the name must survive a power loss as the appends to it will, and the
	// reaps and removals with it; after a sync on the way failed, the
	// directory's answers that same failure, so it is said once
	synced := o.durability.syncDir(o.dir, r, o.dir.Path())
	if why := errors.Join(cmp.Or(unsynced, synced), r.Close()); why != nil {
		return PlacedNotDurable{Placed: placed, Why: why}
	}
	return placed
}

// linking is what linking a create's temp to its name did: linked it, so
// this call made the name; found the name taken; or failed otherwise.
//
//sumtype:decl
type linking interface{ linking() }

type (
	linked   struct{}
	taken    struct{ why error }
	unlinked struct{ why error }
)

func (linked) linking()   {}
func (taken) linking()    {}
func (unlinked) linking() {}

// link links temp to n in r. The name is taken when it is there, and
// when this call's temp is gone and the name holds a regular file: a
// create that placed the name first reaped the temp, so it is the same
// fact, the name taken `[LAW:one-type-per-behavior]`.
func link(r *os.Root, temp string, n Name) linking {
	err := r.Link(temp, n.s)
	switch {
	case err == nil:
		return linked{}
	case errors.Is(err, fs.ErrExist):
		return taken{why: err}
	case errors.Is(err, fs.ErrNotExist):
		if at, lerr := r.Lstat(n.s); lerr == nil && at.Mode().IsRegular() {
			return taken{why: err}
		}
	}
	return unlinked{why: err}
}

// found is nil when n in r holds a regular file, looked at, not followed;
// a name that holds anything else is NotAFile, and one that holds nothing
// is the Lstat's failure.
func (o Disk) found(r *os.Root, n Name) error {
	at, err := r.Lstat(n.s)
	if err != nil {
		return err
	}
	if refusal, refused := aFile.refusal(o.dir.PathOf(n), at.Mode()); refused {
		return refusal
	}
	return nil
}

// reap removes every temp of n in r but own, once n is placed. Then each
// is a crash's leftover, the temp of a create that lost, which finds n
// there when its link fails, or a winner's whose own removal finds it
// gone; so a reap takes nothing a create still needs, and no lock orders
// them `[LAW:no-ambient-temporal-coupling]`.
func (o Disk) reap(r *os.Root, n Name, own string) Reap {
	es, err := o.dir.entries(r)
	if err != nil {
		return Reap{Unlisted: fmt.Errorf("reading %s for the temps of %s: %w", o.dir.Path(), n, err)}
	}
	var rp Reap
	for _, t := range es.temps {
		if t.of != n || t.name == own {
			continue
		}
		switch rm := o.dir.remove(r, t.name).(type) {
		case removed:
			rp.Reaped = append(rp.Reaped, t.name)
		case alreadyGone:
			// another reap removed it first: no one's to report
		case notRemoved:
			rp.Left = append(rp.Left, Leftover{Name: t.name, Why: rm.why})
		}
	}
	return rp
}

// removal is what removing an entry did: removed it, found it already
// gone, which a temp another create's reap removed is, or failed.
//
//sumtype:decl
type removal interface {
	removal()
	// left is the entry name as a leftover when it could not be removed.
	left(name string) []Leftover
}

type (
	removed     struct{}
	alreadyGone struct{}
	notRemoved  struct{ why error }
)

func (removed) removal()                         {}
func (removed) left(string) []Leftover           { return nil }
func (alreadyGone) removal()                     {}
func (alreadyGone) left(string) []Leftover       { return nil }
func (notRemoved) removal()                      {}
func (n notRemoved) left(name string) []Leftover { return []Leftover{{Name: name, Why: n.why}} }

// remove removes name in r through the seam: the one place a removal's
// outcome is decided `[LAW:single-enforcer]`.
func (d Dir) remove(r *os.Root, name string) removal {
	err := d.seams.Remove(r, name)
	switch {
	case err == nil:
		return removed{}
	case errors.Is(err, fs.ErrNotExist):
		return alreadyGone{}
	}
	return notRemoved{why: err}
}

// classified is a directory's entries as the port reads them: the one
// reading of an entry List, Temps and a reap share `[LAW:single-enforcer]`.
type classified struct {
	names   []Name
	temps   []temp
	refused []Refused
}

// temp is a create-once's temporary file: its name in the directory, and
// the name it is a temp of.
type temp struct {
	name string
	of   Name
}

// entries is r's directory's entries, read through the seam and
// classified: a temp of a name, a file of the port, or refused. Only a
// regular file is either, by the entry's type as the directory holds it,
// not following a link: a directory, a symlink or a FIFO named as a temp
// is refused, so no reap ever removes it `[LAW:single-enforcer]`.
func (d Dir) entries(r *os.Root) (c classified, err error) {
	f, err := r.Open(".")
	if err != nil {
		return classified{}, err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	es, err := d.seams.List(d.Path(), f)
	if err != nil {
		return classified{}, err
	}
	for _, e := range es {
		path := d.pathIn(e.Name())
		if r, refused := aFile.refusal(path, e.Type()); refused {
			c.refused = append(c.refused, Refused{Name: e.Name(), Why: r})
			continue
		}
		if of, ok := tempOf(e.Name()); ok {
			c.temps = append(c.temps, temp{name: e.Name(), of: of})
			continue
		}
		n, err := NameOf(e.Name())
		if err != nil {
			c.refused = append(c.refused, Refused{Name: e.Name(), Why: fmt.Errorf("%s: %w", path, err)})
			continue
		}
		c.names = append(c.names, n)
	}
	slices.SortFunc(c.names, func(a, b Name) int { return strings.Compare(a.s, b.s) })
	slices.SortFunc(c.temps, func(a, b temp) int { return strings.Compare(a.name, b.name) })
	return c, nil
}

// pathIn is the path of the entry name in d, as a message names it.
func (d Dir) pathIn(name string) string { return filepath.Join(d.Path(), name) }

// temping is what writing a create's temporary file did: wrote it, or
// failed, and what of it the failure left.
//
//sumtype:decl
type temping interface{ temping() }

type (
	tempWritten   struct{ name string }
	tempUnwritten struct {
		why  error
		left []Leftover
	}
)

func (tempWritten) temping()   {}
func (tempUnwritten) temping() {}

// temp writes b to a new temporary file in r, in the instance's mode,
// named by tempName, which no Name can be. The bytes are on disk before
// the file is closed, so a power loss after the link leaves a whole file,
// never an empty one every append then refuses. os.Root has no
// CreateTemp, so the count runs up from 0 past every name another writer
// holds; two writers of one name each end with their own temp, and the
// link decides whose bytes are kept.
func (o Disk) temp(r *os.Root, n Name, b []byte) temping {
	for i := 0; ; i++ {
		name := tempName(n, i)
		f, err := r.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, o.mode)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return tempUnwritten{why: err}
		}
		_, werr := f.Write(b)
		// the mode is the instance's whatever the umask, so a secret's file
		// is 0600 and a readable one 0644
		if err := errors.Join(werr, f.Chmod(o.mode), o.dir.seams.Sync(o.dir.pathIn(name), f), f.Close()); err != nil {
			return tempUnwritten{why: err, left: o.dir.remove(r, name).left(name)}
		}
		return tempWritten{name: name}
	}
}

func (o Disk) List() (Listing, error) {
	c, err := o.read()
	if err != nil {
		return Listing{}, err
	}
	l := Listing{Names: c.names, Refused: c.refused}
	for _, t := range c.temps {
		l.Temps = append(l.Temps, t.name)
	}
	return l, nil
}

// read is the directory's entries, classified; a missing directory holds
// none, since nothing is created by a read.
func (o Disk) read() (c classified, err error) {
	r, err := o.dir.walk()
	if errors.Is(err, fs.ErrNotExist) {
		return classified{}, nil
	}
	if err != nil {
		return classified{}, err
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	return o.dir.entries(r)
}
