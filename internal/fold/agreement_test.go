package fold_test

import (
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/store"
)

// history is a generated history of three writers, each minting against
// the streams as it last read them, so their frontiers run concurrently.
type history struct {
	w    *world
	r    *rand.Rand
	read map[store.CheckoutID]map[store.Ref]int
}

var writers = []writer{a, a2, b}

func generate(t *testing.T, seed uint64, ops int) *world {
	h := history{w: newWorld(t), r: rand.New(rand.NewPCG(seed, 1)), read: map[store.CheckoutID]map[store.Ref]int{}}
	for range ops {
		h.op(writers[h.r.IntN(len(writers))])
	}
	return h.w
}

// seen is what wr last read, its own stream always whole.
func (h *history) seen(wr writer) fold.State {
	upto := map[store.Ref]int{}
	for _, ref := range h.w.order {
		upto[ref] = h.read[wr.checkout][ref]
	}
	delete(upto, wr.ref())
	return h.w.view(upto)
}

func (h *history) op(wr writer) {
	if h.r.IntN(3) == 0 {
		// wr reads every stream as it stands
		h.read[wr.checkout] = map[store.Ref]int{}
		for _, ref := range h.w.order {
			h.read[wr.checkout][ref] = h.w.head(ref)
		}
	}
	seen := h.seen(wr)
	items := seen.Items()
	if len(items) == 0 {
		h.w.mint(wr, seen, create(events.AtLevel{Level: level(1 + h.r.IntN(3))}))
		return
	}
	it := items[h.r.IntN(len(items))]
	switch h.r.IntN(12) {
	case 0:
		h.w.mint(wr, seen, create(events.AtLevel{Level: level(1 + h.r.IntN(3))}))
	case 1:
		h.w.mint(wr, seen, createUnder(it.ID))
	case 2:
		h.w.mint(wr, seen, events.Act{About: events.Item{ID: it.ID}, Payload: events.FieldSet{Field: title, Value: prose(h.pick("one", "two")), Over: h.over(textStamps(it.Facts.Text(title)))}})
	case 3:
		h.w.mint(wr, seen, events.Act{About: events.Item{ID: it.ID}, Payload: events.FieldSet{Field: priority, Value: plain(h.pick("high", "low", "medium")), Over: h.over(plainStamps(it.Facts.Plain(priority)))}})
	case 4:
		h.w.mint(wr, seen, addLabel(it.ID, label(h.pick("red", "blue"))))
	case 5:
		l := label(h.pick("red", "blue"))
		var observed []events.EventID
		for _, m := range it.Facts.Members(labels) {
			if m.Name == l.Name() {
				observed = stampIDs(m.Added)
			}
		}
		h.w.mint(wr, seen, events.Act{About: events.Item{ID: it.ID}, Payload: events.LabelRm{Label: l, Observed: observed}})
	case 6:
		h.w.mint(wr, seen, comment(it.ID, h.pick("yes", "no")))
	case 7:
		h.w.mint(wr, seen, events.Act{About: events.Item{ID: it.ID}, Payload: events.ItemDelete{}})
	case 8:
		h.w.mint(writer{checkout: wr.checkout, signer: forger{wr.signer}}, seen, set(it.ID, title, prose("forged")))
	case 9:
		h.w.mint(wr, seen, events.Act{About: events.Item{ID: it.ID}, Payload: unknown{}})
	case 10:
		// a record a second stream carries too
		from := h.w.order[h.r.IntN(len(h.w.order))]
		batches := h.w.streams[from].batches
		h.w.append(wr.ref(), batches[h.r.IntN(len(batches))])
	case 11:
		// two facts in one command, one a plain value on the prose field
		h.w.mint(wr, seen, set(it.ID, priority, plain("high")), set(it.ID, title, plain("short")))
	}
}

func (h *history) pick(ss ...string) string { return ss[h.r.IntN(len(ss))] }

// over is every candidate wr saw, half the time, and none otherwise.
func (h *history) over(ids []events.EventID) []events.EventID {
	if h.r.IntN(2) == 0 {
		return nil
	}
	return ids
}

func textStamps(f fold.TextField) []events.EventID {
	switch f := f.(type) {
	case fold.Unset:
	case fold.Agreed:
		return stampIDs(f.By)
	case fold.Conflicted:
		var ids []events.EventID
		for _, c := range f.Candidates {
			ids = append(ids, stampIDs(c.By)...)
		}
		return ids
	}
	return nil
}

func plainStamps(f fold.PlainField) []events.EventID {
	switch f := f.(type) {
	case fold.Unset:
	case fold.Settled:
		ids := []events.EventID{f.By.Event}
		for _, c := range f.Overruled {
			ids = append(ids, stampIDs(c.By)...)
		}
		return ids
	}
	return nil
}

func stampIDs(ss []fold.Stamp) []events.EventID {
	var ids []events.EventID
	for _, s := range ss {
		ids = append(ids, s.Event)
	}
	return ids
}

func TestEveryFoldOfOneHistoryAgrees(t *testing.T) {
	// reached counts the cases the histories reach, so the property cannot
	// pass by generating only easy ones
	var reached struct{ late, duplicates, carried, unapplied, conflicts, overruled, mixed int }
	for seed := range uint64(40) {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			w := generate(t, seed, 30)
			want := w.genesis()
			r := rand.New(rand.NewPCG(seed, 2))

			// Genesis, in an arrival order
			scans := w.scans()
			r.Shuffle(len(scans), func(i, j int) { scans[i], scans[j] = scans[j], scans[i] })
			if got := fold.Genesis(env, fold.Missed{Miss: store.Absent{}}, store.NoSnapshot{}, scans).State; !got.Equal(want.State) {
				t.Errorf("Genesis in another arrival order disagrees")
			}

			// Extend, from a snapshot of a prefix of every stream
			prefix := map[store.Ref]int{}
			var before, delta []fold.Scanned
			for _, ref := range w.order {
				prefix[ref] = r.IntN(w.head(ref) + 1)
				before = append(before, w.span(ref, 0, prefix[ref]))
				delta = append(delta, w.span(ref, prefix[ref], w.head(ref)))
			}
			r.Shuffle(len(delta), func(i, j int) { delta[i], delta[j] = delta[j], delta[i] })
			saved := fold.Genesis(env, fold.Missed{Miss: store.Absent{}}, store.NoSnapshot{}, before)
			snap, err := fold.ReadSnapshot(wrote(t, saved).Payload)
			if err != nil {
				t.Fatal(err)
			}
			extended := fold.Extend(env, snap, wrote(t, saved).Marks, delta)
			reached.late += extended.Facts.Late
			reached.duplicates += want.Facts.Duplicates
			reached.carried += len(want.State.Integrity().Carried)
			reached.unapplied += len(want.State.Integrity().Unapplied)
			reached.conflicts += want.Facts.Counts.ProseConflicts
			reached.overruled += want.Facts.Counts.Overruled
			reached.mixed += want.Facts.Counts.MixedForms
			if !extended.State.Equal(want.State) {
				t.Errorf("Extend from a snapshot of %v disagrees", prefix)
			}
			if !sameMarks(wrote(t, extended).Marks, wrote(t, want).Marks) {
				t.Errorf("Extend saves marks %v; Genesis %v", wrote(t, extended).Marks, wrote(t, want).Marks)
			}

			// Genesis after the fold's tag changed, trusting the verdicts
			bumped := fold.Genesis(env, fold.Missed{Miss: store.TagChanged{}}, store.Saved{Tag: store.FoldTag("lit2 fold 0"), Marks: wrote(t, want).Marks, Payload: wrote(t, want).Payload}, w.scans())
			if !bumped.State.Equal(want.State) {
				t.Errorf("Genesis after a tag change disagrees")
			}
			if bumped.Facts.Checked != want.Facts.Exceptions {
				t.Errorf("Genesis after a tag change checked %d; want the %d exceptions", bumped.Facts.Checked, want.Facts.Exceptions)
			}
		})
	}
	if reached.late == 0 || reached.duplicates == 0 || reached.carried == 0 || reached.unapplied == 0 || reached.conflicts == 0 || reached.overruled == 0 || reached.mixed == 0 {
		t.Errorf("the histories reached %+v; every case should occur", reached)
	}
}

func sameMarks(a, b []store.Mark) bool {
	if len(a) != len(b) {
		return false
	}
	at := map[store.Ref]store.Position{}
	for _, m := range a {
		at[m.Ref] = m.Upto
	}
	for _, m := range b {
		if at[m.Ref] != m.Upto {
			return false
		}
	}
	return true
}
