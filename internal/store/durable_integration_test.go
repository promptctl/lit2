//go:build integration

package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

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
	s := store.Open(me, store.MemoryPorts(files.At(filestest.Seamed(logs, seams), files.NewDurability()), storetest.Entropy(4)), store.DefaultSegmentBound)
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
	got, err := store.Open(me, store.MemoryPorts(files.At(logs, files.NewDurability()), storetest.Entropy(4)), store.DefaultSegmentBound).Temps()
	if err != nil || !slices.Equal(got, []string{temp}) {
		t.Errorf("Temps = %v, %v; want the temp %s said", got, err, temp)
	}
}
