package storetest

import (
	"bytes"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/promptctl/lit2/internal/store"
)

// Files holds fresh, an empty store root each call, to the Files contract.
func Files(t *testing.T, fresh func(*testing.T) store.Files) {
	const p store.Path = "log/a"
	t.Run("an append to no file is NotFound", func(t *testing.T) {
		if _, err := fresh(t).Append(p, []byte("x")); !errors.As(err, new(store.NotFound)) {
			t.Errorf("Append to an absent file failed with %v; want NotFound", err)
		}
	})
	t.Run("appends land in order at the end", func(t *testing.T) {
		f := fresh(t)
		if opened, err := f.CreateOnce(p, []byte("head")); err != nil || opened != store.Created {
			t.Fatalf("CreateOnce = %v, %v; want Created", opened, err)
		}
		for i, want := range []uint64{7, 10} {
			if end, err := f.Append(p, []byte("abc")); err != nil || end != want {
				t.Errorf("append %d ended at %d, %v; want %d", i, end, err, want)
			}
		}
		if b, err := f.ReadAt(p, 0, 100); err != nil || string(b) != "headabcabc" {
			t.Errorf("the file holds %q, %v; want headabcabc", b, err)
		}
		if b, err := f.ReadAt(p, 4, 3); err != nil || string(b) != "abc" {
			t.Errorf("ReadAt(4, 3) = %q, %v; want abc", b, err)
		}
		if size, err := f.Size(p); err != nil || size != 10 {
			t.Errorf("Size = %d, %v; want 10", size, err)
		}
	})
	t.Run("create-once has one winner", func(t *testing.T) {
		f := fresh(t)
		var mu sync.Mutex
		var winners [][]byte
		var wg sync.WaitGroup
		for i := range 16 {
			wg.Go(func() {
				b := []byte{byte('a' + i)}
				opened, err := f.CreateOnce(p, b)
				if err != nil {
					t.Error(err)
					return
				}
				if opened == store.Created {
					mu.Lock()
					winners = append(winners, b)
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		got, err := f.ReadAt(p, 0, 100)
		if err != nil || len(winners) != 1 || !bytes.Equal(got, winners[0]) {
			t.Errorf("16 creations made %d winners and a file holding %q, %v; want one, holding its bytes", len(winners), got, err)
		}
	})
	t.Run("a list holds the files of its directory, and no directory holds none", func(t *testing.T) {
		f := fresh(t)
		if ps, err := f.List("log"); err != nil || len(ps) != 0 {
			t.Errorf("List of an absent directory = %v, %v; want none", ps, err)
		}
		for _, q := range []store.Path{"log/b", "log/a", "other/c"} {
			if _, err := f.CreateOnce(q, []byte("x")); err != nil {
				t.Fatal(err)
			}
		}
		if ps, err := f.List("log"); err != nil || !slices.Equal(ps, []store.Path{"log/a", "log/b"}) {
			t.Errorf("List(log) = %v, %v; want log/a and log/b", ps, err)
		}
	})
}
