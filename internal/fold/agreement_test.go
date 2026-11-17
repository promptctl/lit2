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
	switch h.r.IntN(13) {
	case 0:
		h.w.mint(wr, seen, create(events.AtLevel{Level: level(1 + h.r.IntN(3))}))
	case 1:
		h.w.mint(wr, seen, createUnder(it.ID))
	case 2:
		h.w.mint(wr, seen, events.Act{About: events.Item{ID: it.ID}, Payload: events.FieldSet{Field: title, Value: prose(h.pick("one", "two")), Over: h.over(textStamps(it.Facts.Text(title)))}})
	case 3:
		h.w.mint(wr, seen, events.Act{About: events.Item{ID: it.ID}, Payload: events.FieldSet{Field: priority, Value: plain(h.pick("high", "low", "medium")), Over: h.over(plainStamps(it.Facts.Plain(priority)))}})
	case 4:
		h.w.mint(wr, seen, addLabel(it.ID, name(h.pick("red", "blue"))))
	case 5:
		l := name(h.pick("red", "blue"))
		var observed []events.EventID
		for _, m := range it.Facts.Members(labels) {
			if m.Name == l {
				observed = stampIDs(m.Added)
			}
		}
		h.w.mint(wr, seen, events.Act{About: events.Item{ID: it.ID}, Payload: events.MemberRm{Field: labels, Member: l, Observed: observed}})
	case 6:
		h.w.mint(wr, seen, comment(it.ID, h.pick("yes", "no")))
	case 7:
		h.w.mint(wr, seen, events.Act{About: events.Item{ID: it.ID}, Payload: events.Transition{From: name("backlog"), To: name(h.pick("plan", "done")), Over: it.Facts.Live(fold.Status)}})
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
	case 12:
		// an apply over what wr saw in force, half the time, or over nothing
		var live []events.EventID
		if in, ok := seen.Workspace().(fold.InForce); ok {
			live = in.Live
		}
		h.w.mint(wr, seen, apply(h.pick("prefix = \"one\"", "prefix = \"two\""), h.over(live)...))
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
	var reached struct{ duplicates, carried, unapplied, conflicts, overruled, mixed int }
	for seed := range uint64(40) {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			w := generate(t, seed, 30)
			want := w.replay()
			reached.duplicates += want.Facts.Duplicates
			reached.carried += len(want.State.Integrity().Carried)
			reached.unapplied += len(want.State.Integrity().Unapplied)
			reached.conflicts += want.Facts.Counts.ProseConflicts
			reached.overruled += want.Facts.Counts.Overruled
			reached.mixed += want.Facts.Counts.MixedForms
			r := rand.New(rand.NewPCG(seed, 2))
			for range 4 {
				// every stream arrives in two pieces, split anywhere, the
				// pieces of all streams in any order
				var scans []fold.Scanned
				for _, ref := range w.order {
					at := r.IntN(w.head(ref) + 1)
					scans = append(scans, w.span(ref, 0, at), w.span(ref, at, w.head(ref)))
				}
				r.Shuffle(len(scans), func(i, j int) { scans[i], scans[j] = scans[j], scans[i] })
				if got := fold.Replay(env, scans).State; !got.Equal(want.State) {
					t.Errorf("a replay in another arrival order disagrees")
				}
			}
		})
	}
	if reached.duplicates == 0 || reached.carried == 0 || reached.unapplied == 0 || reached.conflicts == 0 || reached.overruled == 0 || reached.mixed == 0 {
		t.Errorf("the histories reached %+v; every case should occur", reached)
	}
}
