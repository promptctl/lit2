package store_test

import (
	"errors"
	"slices"
	"syscall"
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
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
	return store.Open(me, store.MemoryPorts(filestest.NotDurable(filestest.Memory(), faults), storetest.Entropy(3)), store.DefaultSegmentBound)
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
		return store.Open(me, store.MemoryPorts(f, storetest.Entropy(3)), store.DefaultSegmentBound)
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

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}
