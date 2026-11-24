// Package filestest is the contract suite every implementation of the
// Files port passes.
package filestest

import (
	"bytes"
	"errors"
	"math"
	"slices"
	"sync"
	"testing"

	"github.com/promptctl/lit2/internal/files"
)

// Files holds fresh, an empty directory each call, to the Files contract.
func Files(t *testing.T, fresh func(*testing.T) files.Files) {
	n := files.MustName("a")
	t.Run("an append to no file is NotFound", func(t *testing.T) {
		if _, err := fresh(t).Append(n, []byte("x")); !errors.As(err, new(files.NotFound)) {
			t.Errorf("Append to an absent file failed with %v; want NotFound", err)
		}
	})
	t.Run("appends land in order at the end", func(t *testing.T) {
		f := fresh(t)
		if opened, err := f.CreateOnce(n, []byte("head")); err != nil || opened != files.Created {
			t.Fatalf("CreateOnce = %v, %v; want Created", opened, err)
		}
		for i, want := range []uint64{7, 10} {
			if end, err := f.Append(n, []byte("abc")); err != nil || end != want {
				t.Errorf("append %d ended at %d, %v; want %d", i, end, err, want)
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
		if b, err := f.ReadAt(n, 4, math.MaxUint64); err != nil || string(b) != "abcabc" {
			t.Errorf("ReadAt(4, MaxUint64) = %q, %v; want the tail, abcabc", b, err)
		}
		if b, err := f.ReadAt(n, math.MaxUint64, 1); err != nil || len(b) != 0 {
			t.Errorf("ReadAt(MaxUint64, 1) = %q, %v; want nothing, past the end", b, err)
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
				opened, err := f.CreateOnce(n, b)
				if err != nil {
					t.Error(err)
					return
				}
				if opened == files.Created {
					mu.Lock()
					winners = append(winners, b)
					mu.Unlock()
				}
			})
		}
		wg.Wait()
		got, err := f.ReadAt(n, 0, 100)
		if err != nil || len(winners) != 1 || !bytes.Equal(got, winners[0]) {
			t.Errorf("16 creations made %d winners and a file holding %q, %v; want one, holding its bytes", len(winners), got, err)
		}
	})
	t.Run("a list holds the directory's files, a dot-name among them, and no directory holds none", func(t *testing.T) {
		f := fresh(t)
		if ns, err := f.List(); err != nil || len(ns) != 0 {
			t.Errorf("List of an absent directory = %v, %v; want none", ns, err)
		}
		b, dot := files.MustName("b"), files.MustName(".x")
		for _, m := range []files.Name{b, dot, n} {
			if _, err := f.CreateOnce(m, []byte("x")); err != nil {
				t.Fatal(err)
			}
		}
		if ns, err := f.List(); err != nil || !slices.Equal(ns, []files.Name{dot, n, b}) {
			t.Errorf("List = %v, %v; want .x, a and b", ns, err)
		}
	})
	t.Run("the zero Name is refused by every call, and names no file", func(t *testing.T) {
		f := fresh(t)
		var zero files.Name
		_, appendErr := f.Append(zero, []byte("x"))
		_, createErr := f.CreateOnce(zero, []byte("x"))
		_, readErr := f.ReadAt(zero, 0, 1)
		_, sizeErr := f.Size(zero)
		for call, err := range map[string]error{"Append": appendErr, "CreateOnce": createErr, "ReadAt": readErr, "Size": sizeErr} {
			if !errors.Is(err, files.ErrNotAName) {
				t.Errorf("%s of the zero Name failed with %v; want ErrNotAName", call, err)
			}
		}
		if ns, err := f.List(); err != nil || len(ns) != 0 {
			t.Errorf("after the zero Name's calls the directory holds %v, %v; want nothing", ns, err)
		}
	})
}
