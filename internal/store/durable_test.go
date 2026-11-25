package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

// The rows of what an append says when a sync on its way fails: the frame
// landed, so it is never NotAppended, and never Appended either.

var (
	me                   = store.CheckoutID{0xd}
	errAppend, errCreate = errors.New("the append's fsync failed"), errors.New("the create's fsync failed")
)

// over is a store of me whose logs are a twin answering faults.
func over(faults filestest.Faults) *store.Store {
	repo := git.NewMemory()
	return store.Open(me, store.MemoryPorts(filestest.NotDurable(filestest.Memory(), faults), repo, repo, storetest.Entropy(3)), store.DefaultSegmentBound)
}

// notDurable is a, failing t unless it is AppendedNotDurable by want's
// fault and by none of others.
func notDurable(t *testing.T, a store.Appending, want error, others ...error) store.AppendedNotDurable {
	t.Helper()
	got, ok := a.(store.AppendedNotDurable)
	if !ok || got.Records != 1 || !errors.Is(got.Why, want) {
		t.Fatalf("Append = %#v; want AppendedNotDurable, one record, by %v", a, want)
	}
	for _, o := range others {
		if errors.Is(got.Why, o) {
			t.Errorf("Append's Why %v holds %v; want only %v", got.Why, o, want)
		}
	}
	return got
}

// created fails t unless log is the log's create, its header the log's.
func created(t *testing.T, log store.LogAttempt) {
	t.Helper()
	if c, ok := log.(store.LogCreated); !ok || c.Made != files.Created {
		t.Errorf("the append's log is %#v; want LogCreated, Created", log)
	}
}

// unsynced is log's placement, failing t unless log is LogNotDurable by
// want.
func unsynced(t *testing.T, log store.LogAttempt, want error) store.LogPlaced {
	t.Helper()
	u, ok := log.(store.LogNotDurable)
	if !ok || !errors.Is(u.Why, want) {
		t.Fatalf("the append's log is %#v; want LogNotDurable, by %v", log, want)
	}
	return u.Log
}

func TestAFrameThatLandedIsNeverNotAppended(t *testing.T) {
	bt := must[store.Batch](t)(store.NewBatch(store.Record("landed")))
	t.Run("a failed append's sync is AppendedNotDurable, and the frame reads back once", func(t *testing.T) {
		s := over(filestest.Faults{Append: errAppend})
		got := notDurable(t, s.Append(bt), errAppend, errCreate)
		created(t, got.Log)
		scan := storetest.Whole(t, s, store.WriterRef{Checkout: me})
		if !slices.Equal(storetest.Read(scan), []string{"landed"}) || scan.End != got.Until.At {
			t.Errorf("the log reads %v, ending at %d; want the one frame, ending where the append said, %d", storetest.Read(scan), scan.End, got.Until.At)
		}
	})
	t.Run("a failed sync of the log's create is the frame's, which still lands", func(t *testing.T) {
		s := over(filestest.Faults{CreateOnce: errCreate})
		created(t, unsynced(t, notDurable(t, s.Append(bt), errCreate, errAppend).Log, errCreate))
		if got := storetest.Read(storetest.Whole(t, s, store.WriterRef{Checkout: me})); !slices.Equal(got, []string{"landed"}) {
			t.Errorf("the log reads %v; want the one frame", got)
		}
	})
	t.Run("a log looked up is synced again, and a failed sync is the frame's", func(t *testing.T) {
		errName := errors.New("the log's directory's fsync failed")
		s := over(filestest.Faults{Sync: errName})
		first, ok := s.Append(bt).(store.Appended)
		if !ok {
			t.Fatalf("the first append = %#v; want Appended, its create's sync its own", first)
		}
		created(t, first.Log)
		if got := unsynced(t, notDurable(t, s.Append(bt), errName, errAppend, errCreate).Log, errName); got != (store.LogLookedUp{}) {
			t.Errorf("the second append's log is %#v; want LogLookedUp", got)
		}
	})
}

// broken is logs whose reads fail once a create has run, when read is
// set, and whose appends fail, when appended is: a disk that answers the
// next call wrong after a sync failed.
type broken struct {
	files.Files
	read, appended error
	created        bool
}

func (b *broken) CreateOnce(n files.Name, data []byte) files.Placing {
	b.created = true
	return b.Files.CreateOnce(n, data)
}

func (b *broken) ReadAt(n files.Name, off, size uint64) ([]byte, error) {
	if b.created && b.read != nil {
		return nil, b.read
	}
	return b.Files.ReadAt(n, off, size)
}

func (b *broken) Append(n files.Name, data []byte) files.Appending {
	if b.appended != nil {
		return files.NotAppended{Why: b.appended}
	}
	return b.Files.Append(n, data)
}

// A sync of the log's name that failed is in the answer of an append that
// then landed nothing, so the caller that writes the batch again knows the
// log itself may not survive a power loss.
func TestAFailedSyncOfTheLogsNameIsInAnAppendThatLandedNothing(t *testing.T) {
	bt := must[store.Batch](t)(store.NewBatch(store.Record("lost")))
	open := func(f files.Files) *store.Store {
		repo := git.NewMemory()
		return store.Open(me, store.MemoryPorts(f, repo, repo, storetest.Entropy(3)), store.DefaultSegmentBound)
	}
	notAppended := func(t *testing.T, a store.Appending, want error) store.NotAppended {
		t.Helper()
		got, ok := a.(store.NotAppended)
		if !ok || !errors.Is(got.Why, want) {
			t.Fatalf("Append = %#v; want NotAppended, by %v", a, want)
		}
		return got
	}
	t.Run("the create's sync fails, then the log's id does not read", func(t *testing.T) {
		errRead := errors.New("the log's header did not read")
		s := open(&broken{Files: filestest.NotDurable(filestest.Memory(), filestest.Faults{CreateOnce: syscall.EIO}), read: errRead})
		created(t, unsynced(t, notAppended(t, s.Append(bt), errRead).Log, syscall.EIO))
	})
	t.Run("the lookup's sync fails, then the frame does not land", func(t *testing.T) {
		errWrite := errors.New("the frame did not write")
		logs := filestest.Memory()
		if _, ok := open(logs).Append(bt).(store.Appended); !ok {
			t.Fatal("the append that makes the log did not land")
		}
		s := open(&broken{Files: filestest.NotDurable(logs, filestest.Faults{Sync: syscall.EIO}), appended: errWrite})
		if got := unsynced(t, notAppended(t, s.Append(bt), errWrite).Log, syscall.EIO); got != (store.LogLookedUp{}) {
			t.Errorf("the append's log is %#v; want LogLookedUp", got)
		}
	})
}

// An append that places no log says what its create left.
func TestAnAppendThatPlacesNoLogSaysItsCreatesTemp(t *testing.T) {
	logs := files.DirAt(t.TempDir(), files.MustName("log"))
	if err := files.NewDurability().Make(logs); err != nil {
		t.Fatal(err)
	}
	temp := ".tmp-" + store.LogName(me).String() + "-0"
	errRemove := errors.New("the directory refused the removal")
	seams := filestest.Recording(&filestest.Syncs{}, func(path string) error {
		if path == filepath.Join(logs.Path(), temp) {
			return errCreate
		}
		return nil
	})
	seams.Remove = func(*os.Root, string) error { return errRemove }
	repo := git.NewMemory()
	s := store.Open(me, store.MemoryPorts(files.At(filestest.Seamed(logs, seams), files.NewDurability()), repo, repo, storetest.Entropy(4)), store.DefaultSegmentBound)
	got, ok := s.Append(must[store.Batch](t)(store.NewBatch(store.Record("lost")))).(store.NotAppended)
	if !ok || !errors.Is(got.Why, errCreate) {
		t.Fatalf("Append whose create's temp would not sync = %#v; want NotAppended, by that sync", got)
	}
	if no, ok := got.Log.(store.NoLog); !ok || len(no.Left) != 1 || no.Left[0].Name != temp || !errors.Is(no.Left[0].Why, errRemove) {
		t.Errorf("the append's log is %#v; want NoLog, %s Left by the removal", got.Log, temp)
	}
}

// A log create's temp that no create of its name reaped is said by the
// store, never dropped.
func TestTheStoreSaysTheLogsTemps(t *testing.T) {
	base := t.TempDir()
	logs := files.DirAt(base, files.MustName("log"))
	if err := files.NewDurability().Make(logs); err != nil {
		t.Fatal(err)
	}
	temp := ".tmp-" + store.LogName(me).String() + "-0"
	if err := os.WriteFile(filepath.Join(logs.Path(), temp), []byte("a crash's"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := git.NewMemory()
	got, err := store.Open(me, store.MemoryPorts(files.At(logs, files.NewDurability()), repo, repo, storetest.Entropy(4)), store.DefaultSegmentBound).Temps()
	if err != nil || !slices.Equal(got, []string{temp}) {
		t.Errorf("Temps = %v, %v; want the temp %s said", got, err, temp)
	}
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}
