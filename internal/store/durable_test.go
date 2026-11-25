package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
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
	return store.Open(me, store.MemoryPorts(filestest.NotDurable(files.NewMemory(), faults), repo, repo, storetest.Entropy(3)), store.DefaultSegmentBound)
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
func created(t *testing.T, log store.LogPlacement) {
	t.Helper()
	if c, ok := log.(store.LogCreated); !ok || c.Made != files.Created {
		t.Errorf("the append's log is %#v; want LogCreated, Created", log)
	}
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
		created(t, notDurable(t, s.Append(bt), errCreate, errAppend).Log)
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
		if got := notDurable(t, s.Append(bt), errName, errAppend, errCreate); got.Log != (store.LogLookedUp{}) {
			t.Errorf("the second append's log is %#v; want LogLookedUp", got.Log)
		}
	})
}

// An append that places no log says what its create left.
func TestAnAppendThatPlacesNoLogSaysItsCreatesTemp(t *testing.T) {
	logs := files.DirAt(t.TempDir(), files.MustName("log"))
	if err := logs.Make(); err != nil {
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
	s := store.Open(me, store.MemoryPorts(files.At(filestest.Seamed(logs, seams)), repo, repo, storetest.Entropy(4)), store.DefaultSegmentBound)
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
	if err := logs.Make(); err != nil {
		t.Fatal(err)
	}
	temp := ".tmp-" + store.LogName(me).String() + "-0"
	if err := os.WriteFile(filepath.Join(logs.Path(), temp), []byte("a crash's"), 0o644); err != nil {
		t.Fatal(err)
	}
	repo := git.NewMemory()
	got, err := store.Open(me, store.MemoryPorts(files.At(logs), repo, repo, storetest.Entropy(4)), store.DefaultSegmentBound).Temps()
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
