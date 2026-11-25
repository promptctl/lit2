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
func notDurable(t *testing.T, a store.Appending, made files.Made, want error, others ...error) store.AppendedNotDurable {
	t.Helper()
	got, ok := a.(store.AppendedNotDurable)
	if !ok || got.Records != 1 || got.Log != made || !errors.Is(got.Why, want) {
		t.Fatalf("Append = %#v; want AppendedNotDurable, one record, the log %v, by %v", a, made, want)
	}
	for _, o := range others {
		if errors.Is(got.Why, o) {
			t.Errorf("Append's Why %v holds %v; want only %v", got.Why, o, want)
		}
	}
	return got
}

func TestAFrameThatLandedIsNeverNotAppended(t *testing.T) {
	bt := must[store.Batch](t)(store.NewBatch(store.Record("landed")))
	t.Run("a failed append's sync is AppendedNotDurable, and the frame reads back once", func(t *testing.T) {
		s := over(filestest.Faults{Append: errAppend})
		got := notDurable(t, s.Append(bt), files.Created, errAppend, errCreate)
		scan := storetest.Whole(t, s, store.WriterRef{Checkout: me})
		if !slices.Equal(storetest.Read(scan), []string{"landed"}) || scan.End != got.Until.At {
			t.Errorf("the log reads %v, ending at %d; want the one frame, ending where the append said, %d", storetest.Read(scan), scan.End, got.Until.At)
		}
	})
	t.Run("a failed sync of the log's create is the frame's, which still lands", func(t *testing.T) {
		s := over(filestest.Faults{CreateOnce: errCreate})
		notDurable(t, s.Append(bt), files.Created, errCreate, errAppend)
		if got := storetest.Read(storetest.Whole(t, s, store.WriterRef{Checkout: me})); !slices.Equal(got, []string{"landed"}) {
			t.Errorf("the log reads %v; want the one frame", got)
		}
	})
}

// A log create's temp that no create of its name reaped is said by Heads,
// never dropped.
func TestHeadsSaysTheLogsTemps(t *testing.T) {
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
	h, err := store.Open(me, store.MemoryPorts(files.At(logs), repo, repo, storetest.Entropy(4)), store.DefaultSegmentBound).Heads()
	if err != nil || !slices.Equal(h.Temps, []string{temp}) {
		t.Errorf("Heads = %+v, %v; want the temp %s said", h, err, temp)
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
