package store_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/git/gittest"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

// BenchmarkAFoldReadsEveryPublishedStream is the store's part of a fold
// over a store past budgets.md's 10x depth: ten writers of twenty thousand
// events each, an event a few hundred bytes, read by a clone that fetched
// lit's repository and holds no log. Heads is the staleness check, every
// writer's ref head; Read is every stream read whole after it, as a full
// refold reads. Each run is one invocation's, with its own git.
func BenchmarkAFoldReadsEveryPublishedStream(b *testing.B) {
	const writers, batches, perBatch = 10, 2000, 10
	c := gittest.Clone(b)
	setup := gittest.Runner(b)
	for w := range writers {
		s := store.Open(store.CheckoutID{byte(w + 1)}, store.DiskPorts(setup, c, storetest.Entropy(byte(w))), store.DefaultSegmentBound)
		for i := range batches {
			records := make([]store.Record, perBatch)
			for r := range records {
				records[r] = store.Record(fmt.Sprintf("%d-%05d-%d %s", w, i, r, strings.Repeat("x", 300)))
			}
			bt, err := store.NewBatch(records...)
			if err != nil {
				b.Fatal(err)
			}
			if _, err := s.Append(bt); err != nil {
				b.Fatal(err)
			}
		}
		sealed, err := s.SealOwn()
		if err != nil {
			b.Fatal(err)
		}
		if _, err := s.Publish(sealed.(store.Publication)); err != nil {
			b.Fatal(err)
		}
	}
	open := func() *store.Store {
		ports := store.DiskPorts(gittest.Runner(b), c, storetest.Entropy(0))
		ports.Files = store.NewMemoryFiles()
		return store.Open(store.CheckoutID{0xff}, ports, store.DefaultSegmentBound)
	}
	heads := func(s *store.Store) store.Heads {
		h, err := s.Heads()
		if err != nil || len(h.Streams) != writers {
			b.Fatalf("heads = %+v, %v; want %d streams", h, err, writers)
		}
		return h
	}
	b.Run("Heads", func(b *testing.B) {
		for b.Loop() {
			s := open()
			heads(s)
		}
	})
	b.Run("Read", func(b *testing.B) {
		for b.Loop() {
			s := open()
			frames := 0
			for _, st := range heads(s).Streams {
				scan, err := s.Read(store.Range{Stream: st})
				if err != nil {
					b.Fatal(err)
				}
				frames += len(scan.Frames)
			}
			if frames != writers*batches {
				b.Fatalf("a fold read %d frames; want %d", frames, writers*batches)
			}
		}
	})
}
