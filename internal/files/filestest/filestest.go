// Package filestest is the contract suite the Files port's disk adapter
// and its twin both pass, run side by side, so one contract is what both
// are `[LAW:one-source-of-truth]` (D2); and what a test outside files
// needs of the port: a Dir whose seams it sees or fails, a twin whose
// writes land but are not durable, and the writes a test arranges state
// with.
package filestest

import (
	"bytes"
	"errors"
	"math"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/internal/seam"
)

// Files holds the disk adapter, over a fresh temporary directory, and the
// twin to the Files contract.
func Files(t *testing.T) {
	t.Run("disk", func(t *testing.T) {
		contract(t, func(t *testing.T) files.Files {
			return files.At(files.DirAt(t.TempDir(), files.MustName("dir")), files.NewDurability())
		})
	})
	t.Run("memory", func(t *testing.T) {
		contract(t, func(*testing.T) files.Files { return Memory() })
	})
}

// contract holds fresh, an empty directory each call, to the Files
// contract. Every row names the variant a write answers, never only
// whether it failed.
func contract(t *testing.T, fresh func(*testing.T) files.Files) {
	n := files.MustName("a")
	t.Run("an append to no file is NotAppended, NotFound", func(t *testing.T) {
		got := fresh(t).Append(n, []byte("x"))
		if no, ok := got.(files.NotAppended); !ok || !errors.As(no.Why, new(files.NotFound)) {
			t.Errorf("Append to an absent file = %#v; want NotAppended, NotFound", got)
		}
	})
	t.Run("appends land in order at the end, durable", func(t *testing.T) {
		f := fresh(t)
		if got := f.CreateOnce(n, []byte("head")); !placed(got, files.Created) {
			t.Fatalf("CreateOnce = %#v; want Placed, Created, reaping nothing", got)
		}
		for i, want := range []uint64{7, 10} {
			if got := f.Append(n, []byte("abc")); got != (files.Appended{End: want}) {
				t.Errorf("append %d = %#v; want Appended at %d", i, got, want)
			}
		}
		if b, err := f.ReadAt(n, 0, 100); err != nil || string(b) != "headabcabc" {
			t.Errorf("the file holds %q, %v; want headabcabc", b, err)
		}
		if b, err := f.ReadAt(n, 4, 3); err != nil || string(b) != "abc" {
			t.Errorf("ReadAt(4, 3) = %q, %v; want abc", b, err)
		}
		if size, err := f.Size(n); err != nil || size != 10 {
			t.Errorf("Size = %d, %v; want 10", size, err)
		}
		if b, err := f.ReadAt(n, 7, 100); err != nil || string(b) != "abc" {
			t.Errorf("ReadAt(7, 100) = %q, %v; want fewer at the end of the file, abc", b, err)
		}
		if b, err := f.ReadAt(n, math.MaxUint64, 1); err != nil || len(b) != 0 {
			t.Errorf("ReadAt(MaxUint64, 1) = %q, %v; want nothing, past the end", b, err)
		}
	})
	t.Run("a read of all is the whole file, and of no file is NotFound", func(t *testing.T) {
		f := fresh(t)
		if b, err := f.ReadAll(n); !errors.As(err, new(files.NotFound)) {
			t.Errorf("ReadAll of an absent file = %q, %v; want NotFound", b, err)
		}
		Create(t, f, n, []byte("head"))
		Append(t, f, n, []byte("abc"))
		if b, err := f.ReadAll(n); err != nil || string(b) != "headabc" {
			t.Errorf("ReadAll = %q, %v; want headabc", b, err)
		}
	})
	t.Run("a second create-once finds the first's file, durable, and keeps its bytes", func(t *testing.T) {
		f := fresh(t)
		Create(t, f, n, []byte("first"))
		if got := f.CreateOnce(n, []byte("second")); !placed(got, files.Existing) {
			t.Errorf("the second CreateOnce = %#v; want Placed, Existing, reaping nothing", got)
		}
		if b, err := f.ReadAll(n); err != nil || string(b) != "first" {
			t.Errorf("the file holds %q, %v; want the first's bytes", b, err)
		}
	})
	t.Run("create-once has one winner, and every loser finds its file durable", func(t *testing.T) {
		f := fresh(t)
		var mu sync.Mutex
		var winners [][]byte
		var wg sync.WaitGroup
		for i := range 16 {
			wg.Go(func() {
				b := []byte{byte('a' + i)}
				got, ok := f.CreateOnce(n, b).(files.Placed)
				if !ok {
					t.Errorf("CreateOnce = %#v; want Placed", got)
					return
				}
				if got.Made == files.Created {
					mu.Lock()
					winners = append(winners, b)
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		got, err := f.ReadAll(n)
		if err != nil || len(winners) != 1 || !bytes.Equal(got, winners[0]) {
			t.Errorf("16 creations made %d winners and a file holding %q, %v; want one, holding its bytes", len(winners), got, err)
		}
		if l, err := f.List(); err != nil || len(l.Temps) != 0 {
			t.Errorf("after 16 creations the directory holds %v, %v; want no temp", l, err)
		}
	})
	t.Run("concurrent appends to one file each land whole where their answer says, and none is lost", func(t *testing.T) {
		f := fresh(t)
		Create(t, f, n, []byte("head"))
		const appenders, size = 16, 64
		ends := make([]uint64, appenders)
		var wg sync.WaitGroup
		for i := range appenders {
			wg.Go(func() {
				got, ok := f.Append(n, bytes.Repeat([]byte{byte('a' + i)}, size)).(files.Appended)
				if !ok {
					t.Errorf("Append = %#v; want Appended", got)
				}
				ends[i] = got.End
			})
		}
		wg.Wait()
		if s, err := f.Size(n); err != nil || s != 4+appenders*size {
			t.Errorf("after %d appends of %d bytes the file is %d bytes, %v; want %d", appenders, size, s, err, 4+appenders*size)
		}
		for i, end := range ends {
			if b, err := f.ReadAt(n, end-size, size); err != nil || !bytes.Equal(b, bytes.Repeat([]byte{byte('a' + i)}, size)) {
				t.Errorf("append %d's bytes before its end %d are %q, %v; want its frame whole", i, end, b, err)
			}
		}
	})
	t.Run("a list holds the directory's files, a dot-name among them, and no directory holds none", func(t *testing.T) {
		f := fresh(t)
		if l, err := f.List(); err != nil || len(l.Names)+len(l.Temps)+len(l.Refused) != 0 {
			t.Errorf("List of an absent directory = %v, %v; want none", l, err)
		}
		b, dot := files.MustName("b"), files.MustName(".x")
		for _, m := range []files.Name{b, dot, n} {
			Create(t, f, m, []byte("x"))
		}
		if l, err := f.List(); err != nil || !slices.Equal(l.Names, []files.Name{dot, n, b}) || len(l.Temps) != 0 || len(l.Refused) != 0 {
			t.Errorf("List = %v, %v; want .x, a and b, and no temp, and nothing refused", l, err)
		}
	})
	t.Run("the zero Name is refused by every call, and names no file", func(t *testing.T) {
		f := fresh(t)
		var zero files.Name
		appended, _ := f.Append(zero, []byte("x")).(files.NotAppended)
		created, _ := f.CreateOnce(zero, []byte("x")).(files.NotPlaced)
		_, readErr := f.ReadAt(zero, 0, 1)
		_, readAllErr := f.ReadAll(zero)
		_, sizeErr := f.Size(zero)
		for call, err := range map[string]error{"Append": appended.Why, "CreateOnce": created.Why, "ReadAt": readErr, "ReadAll": readAllErr, "Size": sizeErr} {
			if !errors.Is(err, files.ErrNotAName) {
				t.Errorf("%s of the zero Name failed with %v; want ErrNotAName, NotAppended or NotPlaced for a write", call, err)
			}
		}
		if l, err := f.List(); err != nil || len(l.Names)+len(l.Temps)+len(l.Refused) != 0 {
			t.Errorf("after the zero Name's calls the directory holds %v, %v; want nothing", l, err)
		}
	})
}

// placed is whether p is Placed as made, reaping nothing.
func placed(p files.Placing, made files.Made) bool {
	got, ok := p.(files.Placed)
	return ok && got.Made == made && len(got.Reap.Reaped) == 0 && len(got.Reap.Left) == 0 && got.Reap.Unlisted == nil
}

// Create creates n holding b in f, failing t unless it is Placed.
func Create(t testing.TB, f files.Files, n files.Name, b []byte) files.Placed {
	t.Helper()
	got, ok := f.CreateOnce(n, b).(files.Placed)
	if !ok {
		t.Fatalf("CreateOnce(%s) = %#v; want Placed", n, got)
	}
	return got
}

// Append appends b to n in f, failing t unless it is Appended.
func Append(t testing.TB, f files.Files, n files.Name, b []byte) files.Appended {
	t.Helper()
	got, ok := f.Append(n, b).(files.Appended)
	if !ok {
		t.Fatalf("Append(%s) = %#v; want Appended", n, got)
	}
	return got
}

// Memory is an empty directory in process, standing for /memory: for a
// test whose files' paths name nothing it reads.
func Memory() *files.Memory { return files.NewMemory(files.DirAt("/", files.MustName("memory"))) }

// Seams is a Dir's seams, for a test outside files to see or fail its
// fsyncs, gate its steps, or fail its removals.
type Seams = seam.Seams

// OS is the seams of every Dir but a test's, for a test to change one of.
func OS() Seams { return seam.OS() }

// Seamed is d with s at its seams.
func Seamed(d files.Dir, s Seams) files.Dir { return d.Seamed(s) }

// Syncs is a record of the fsyncs a Dir's calls ran, by path, in order,
// safe for concurrent use.
type Syncs struct {
	mu    sync.Mutex
	paths []string
}

// Seen is s's record so far.
func (s *Syncs) Seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.paths)
}

// Recording is the OS's seams with every fsync recorded in s, then run,
// unless fail answers an error for its path, which the sync is then
// instead.
func Recording(s *Syncs, fail func(path string) error) Seams {
	seams := seam.OS()
	run := seams.Sync
	seams.Sync = func(path string, f *os.File) error {
		s.mu.Lock()
		s.paths = append(s.paths, path)
		s.mu.Unlock()
		if err := fail(path); err != nil {
			return err
		}
		return run(path, f)
	}
	return seams
}

// Passing is a fail that fails no sync.
func Passing(string) error { return nil }

// Faults is the sync failure a NotDurable twin answers for each call that
// syncs: an Append's, a CreateOnce's, a Sync's, or none where nil.
type Faults struct{ Append, CreateOnce, Sync error }

// NotDurable is f whose writes land, and then answer the fault their
// operation is given as a failed sync: the state of a disk whose fsync
// failed after the bytes reached it. Every other call is f's.
func NotDurable(f files.Files, faults Faults) files.Files {
	return notDurable{Files: f, faults: faults}
}

type notDurable struct {
	files.Files
	faults Faults
}

func (d notDurable) Append(n files.Name, b []byte) files.Appending {
	got := d.Files.Append(n, b)
	if a, landed := got.(files.Appended); landed && d.faults.Append != nil {
		return files.AppendedNotDurable{Appended: a, Why: d.faults.Append}
	}
	return got
}

func (d notDurable) CreateOnce(n files.Name, b []byte) files.Placing {
	got := d.Files.CreateOnce(n, b)
	if p, landed := got.(files.Placed); landed && d.faults.CreateOnce != nil {
		return files.PlacedNotDurable{Placed: p, Why: d.faults.CreateOnce}
	}
	return got
}

func (d notDurable) Sync() error {
	if err := d.Files.Sync(); err != nil || d.faults.Sync == nil {
		return err
	}
	return d.faults.Sync
}
