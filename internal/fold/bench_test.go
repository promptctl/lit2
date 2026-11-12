package fold_test

import (
	"fmt"
	"math/rand/v2"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/store"
)

// scale is scenario 13's store as the fold reads it: 10,000 items and
// 200,000 events from ten writers, each writer's records its own stream;
// the saved fold of it; and 100 events two stale writers mint after it,
// sorting among what the snapshot already holds.
type scale struct {
	scans []fold.Scanned
	saved fold.Folded
	delta []fold.Scanned
}

var corpus = sync.OnceValue(func() scale {
	const items, total, writers = 10_000, 200_000, 10
	r, salts := rand.New(rand.NewPCG(13, 13)), rand.NewChaCha8([32]byte{13})
	signers := make([]events.Signer, writers)
	for w := range signers {
		signers[w] = keyOf(byte(w + 1)).Signer()
	}
	checkout := func(w int) store.CheckoutID { return store.CheckoutID{0xc, byte(w)} }
	batches := make([][]store.Batch, writers)
	var f, half events.Frontier
	var ids []events.ItemID
	var names events.NamesBuilder
	lastTitle := map[events.ItemID]events.EventID{}
	lastLabel := map[events.ItemID]events.EventID{}
	mint := func(w int, f events.Frontier, n events.Names, d events.Draft) events.Event {
		m, err := events.Mint(events.Context{Writer: checkout(w), Agent: events.Unlabeled{}, ConfigRef: configRef, Policy: 1,
			Frontier: f, Names: n, Now: t0}, events.NewDrafts(d), signers[w], salts)
		if err != nil {
			panic(err)
		}
		batches[w] = append(batches[w], m.Batch)
		return m.Events[0]
	}
	act := func(r *rand.Rand, it events.ItemID) events.Draft {
		switch k := r.IntN(20); {
		case k < 9:
			var over []events.EventID
			if last, ok := lastTitle[it]; ok {
				over = []events.EventID{last}
			}
			return events.Act{About: events.Item{ID: it}, Payload: events.FieldSet{Field: title, Value: prose(fmt.Sprint("title ", r.Uint64())), Over: over}}
		case k < 13:
			return events.Act{About: events.Item{ID: it}, Payload: events.FieldSet{Field: priority, Value: plain([]string{"high", "low", "medium"}[r.IntN(3)])}}
		case k < 17:
			return comment(it, fmt.Sprint("a comment ", r.Uint64()))
		case k < 19:
			return addLabel(it, label("red"))
		default:
			var observed []events.EventID
			if last, ok := lastLabel[it]; ok {
				observed = []events.EventID{last}
			}
			return events.Act{About: events.Item{ID: it}, Payload: events.LabelRm{Label: label("red"), Observed: observed}}
		}
	}
	for n := 0; n < total; n++ {
		w := r.IntN(writers)
		if n == total/2 {
			half = f
		}
		var e events.Event
		if len(ids) == 0 || r.IntN(total-n) < items-len(ids) {
			e = mint(w, f, names.Names(), create(events.AtLevel{Level: level(1 + r.IntN(3))}))
			id := idOf(e)
			ids = append(ids, id)
			names.Add(top, id.Segments()[0])
		} else {
			it := ids[r.IntN(len(ids))]
			e = mint(w, f, names.Names(), act(r, it))
			switch p := e.Body().(events.Readable).Payload.(type) {
			case events.FieldSet:
				if p.Field == title {
					lastTitle[it] = e.ID()
				}
			case events.LabelAdd:
				lastLabel[it] = e.ID()
			case events.ItemCreate, events.ItemDelete, events.LabelRm, events.CommentAdd:
			}
		}
		f = f.Observe(e.Header())
	}
	var c scale
	var held []int
	for w := range writers {
		held = append(held, len(batches[w]))
	}
	c.scans = streams(batches, held, nil)
	c.saved = fold.Genesis(must(fold.NewEnv(env.Registry(), env.Suites(), runtime.GOMAXPROCS(0))), store.Absent{}, nil, store.NoSnapshot{}, c.scans)
	// two writers that last read the store at its half mint 100 acts
	for i := range 100 {
		mint(i%2, half, names.Names(), act(r, ids[r.IntN(len(ids)/2)]))
	}
	c.delta = streams(batches, nil, held)
	return c
})

// streams is each writer's batches as one stream's scan: every batch up
// to held[w] when held is set, else every batch from from[w].
func streams(batches [][]store.Batch, held, from []int) []fold.Scanned {
	var out []fold.Scanned
	for w, bs := range batches {
		start, end := 0, len(bs)
		if held != nil {
			end = held[w]
		}
		if from != nil {
			start = from[w]
		}
		var all []byte
		var at store.Offset
		for i, b := range bs[:end] {
			if i == start {
				at = store.Offset(len(all))
			}
			all = append(all, store.Encode(b)...)
		}
		if start == end {
			at = store.Offset(len(all))
		}
		ref := store.WriterRef{Checkout: store.CheckoutID{0xc, byte(w)}}
		r := store.Range{Stream: store.Stream{Ref: ref, Until: store.Position{Log: store.LogID{0xc, byte(w)}, At: store.Offset(len(all))}}, From: at}
		out = append(out, fold.Scanned{Range: r, Scan: store.ScanFrames(at, all[at:])})
	}
	return out
}

// budget fails b when one run took longer than limit.
func budget(b *testing.B, limit time.Duration) {
	if per := b.Elapsed() / time.Duration(b.N); per > limit {
		b.Errorf("one run took %v; the budget is %v", per, limit)
	}
}

// BenchmarkFoldGenesisTrusted is a full refold after a fold-version bump:
// every verdict trusted from the snapshot it replaces, the cold and the
// bump budgets' worst case.
func BenchmarkFoldGenesisTrusted(b *testing.B) {
	c := corpus()
	file := store.Saved{Tag: store.FoldTag("lit2 fold 0"), Marks: wrote(b, c.saved).Marks, Payload: wrote(b, c.saved).Payload}
	b.ResetTimer()
	for b.Loop() {
		f := fold.Genesis(env, store.TagChanged{}, file.Marks, file, c.scans)
		if f.Facts.Checked != 0 || f.Facts.Records != 200_000 {
			b.Fatalf("a trusted genesis checked %d of %d records", f.Facts.Checked, f.Facts.Records)
		}
	}
	budget(b, time.Second)
}

// BenchmarkFoldExtendInterleaved is the warm read: load the snapshot, fold
// 100 events that sort among what it holds, and encode the next.
func BenchmarkFoldExtendInterleaved(b *testing.B) {
	c := corpus()
	b.ResetTimer()
	for b.Loop() {
		snap, err := fold.ReadSnapshot(wrote(b, c.saved).Payload)
		if err != nil {
			b.Fatal(err)
		}
		f := fold.Extend(env, snap, wrote(b, c.saved).Marks, c.delta)
		if f.Facts.Checked != 100 || f.Facts.Late != 100 || len(wrote(b, f).Payload) == 0 {
			b.Fatalf("an interleaved extend checked %d, late %d", f.Facts.Checked, f.Facts.Late)
		}
	}
	budget(b, 300*time.Millisecond)
}

// BenchmarkFoldGenesisUncached is first adoption: every signature checked
// once, on every core. It is printed as the one-time cost, not gated.
func BenchmarkFoldGenesisUncached(b *testing.B) {
	c := corpus()
	all := must(fold.NewEnv(env.Registry(), env.Suites(), runtime.GOMAXPROCS(0)))
	b.ResetTimer()
	for b.Loop() {
		f := fold.Genesis(all, store.Absent{}, nil, store.NoSnapshot{}, c.scans)
		if f.Facts.Checked != 200_000 {
			b.Fatalf("an uncached genesis checked %d", f.Facts.Checked)
		}
	}
	b.ReportMetric(float64(runtime.GOMAXPROCS(0)), "cores")
}
