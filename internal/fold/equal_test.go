package fold_test

import (
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/store"
)

// base is a world holding one of each fact: an item with a title, a
// label, a comment, a carried, an unapplied and a malformed record.
func base(t *testing.T) (*world, events.ItemID, events.Event, events.Event) {
	w, id := item(t)
	named := w.mint(a, w.state(), set(id, title, prose("t")))[0]
	red := w.mint(a, w.state(), addLabel(id, label("red")))[0]
	w.mint(b, w.state(), comment(id, "c"))
	w.mint(writer{checkout: checkoutB, signer: forger{keyB.Signer()}}, w.state(), set(id, priority, plain("x")))
	w.mint(b, w.state(), events.Act{About: events.Item{ID: id}, Payload: unknown{}})
	w.append(a.ref(), must(store.NewBatch(store.Record("not an event"))))
	return w, id, named, red
}

func TestEqualitySeesEveryFact(t *testing.T) {
	for _, c := range []struct {
		name string
		add  func(w *world, id events.ItemID, named, red events.Event)
	}{
		{"another item", func(w *world, _ events.ItemID, _, _ events.Event) {
			w.mint(a, w.state(), create(events.AtLevel{Level: level(2)}))
		}},
		{"a delete", func(w *world, id events.ItemID, _, _ events.Event) {
			w.mint(a, w.state(), events.Act{About: events.Item{ID: id}, Payload: events.ItemDelete{}})
		}},
		{"a second create", func(w *world, id events.ItemID, _, _ events.Event) {
			w.mint(b, fold.State{}, events.Act{About: events.Item{ID: id}, Payload: events.ItemCreate{Place: events.AtLevel{Level: level(1)}}})
		}},
		{"a text over the title", func(w *world, id events.ItemID, named, _ events.Event) {
			w.mint(a, w.state(), set(id, title, prose("u"), named))
		}},
		{"a text beside the title", func(w *world, id events.ItemID, _, _ events.Event) {
			w.mint(a, w.state(), set(id, title, prose("u")))
		}},
		{"the same text by another writer", func(w *world, id events.ItemID, _, _ events.Event) {
			w.mint(b, w.state(), set(id, title, prose("t")))
		}},
		{"a plain value", func(w *world, id events.ItemID, _, _ events.Event) {
			w.mint(a, w.state(), set(id, priority, plain("high")))
		}},
		{"a label", func(w *world, id events.ItemID, _, _ events.Event) {
			w.mint(a, w.state(), addLabel(id, label("blue")))
		}},
		{"the label again", func(w *world, id events.ItemID, _, _ events.Event) {
			w.mint(b, w.state(), addLabel(id, label("red")))
		}},
		{"a label removed", func(w *world, id events.ItemID, _, red events.Event) {
			w.mint(a, w.state(), rmLabel(id, label("red"), red))
		}},
		{"a remove that saw nothing", func(w *world, id events.ItemID, _, _ events.Event) {
			w.mint(a, w.state(), rmLabel(id, label("red")))
		}},
		{"a comment", func(w *world, id events.ItemID, _, _ events.Event) {
			w.mint(a, w.state(), comment(id, "d"))
		}},
		{"a forged event", func(w *world, id events.ItemID, _, _ events.Event) {
			w.mint(writer{checkout: checkoutA, signer: strange{keyA.Signer()}}, w.state(), set(id, title, prose("u")))
		}},
		{"an unknown kind", func(w *world, id events.ItemID, _, _ events.Event) {
			w.mint(a, w.state(), events.Act{About: events.Item{ID: id}, Payload: unknown{}})
		}},
		{"a misaddressed comment", func(w *world, id events.ItemID, _, _ events.Event) {
			w.mint(a, w.state(), events.Act{About: events.Item{ID: id}, Payload: events.CommentAdd{Body: events.SealPlain("x")}})
		}},
		{"a malformed record", func(w *world, _ events.ItemID, _, _ events.Event) {
			w.append(b.ref(), must(store.NewBatch(store.Record("nor this"))))
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, id, named, red := base(t)
			was := w.genesis()
			c.add(w, id, named, red)
			now := w.genesis()
			if now.State.Equal(was.State) || was.State.Equal(now.State) {
				t.Errorf("a state with %s is Equal to one without", c.name)
			}
			for _, f := range []fold.Folded{was, now} {
				snap, err := fold.ReadSnapshot(f.Save.Payload)
				if err != nil {
					t.Fatal(err)
				}
				if !snap.State.Equal(f.State) {
					t.Errorf("the codec does not round-trip a state with %s", c.name)
				}
			}
		})
	}
}

func TestTheSnapshotCarriesWhereItStopped(t *testing.T) {
	empty, err := fold.ReadSnapshot(fold.Genesis(env, fold.Missed{Miss: store.Absent{}}, store.NoSnapshot{}, nil).Save.Payload)
	if err != nil || empty.Through != (fold.Nothing{}) || !empty.State.Equal(fold.State{}) {
		t.Errorf("a fold of nothing saves %#v, %v", empty, err)
	}
	w, _, _, _ := base(t)
	evs := sortedEvents(w)
	snap, err := fold.ReadSnapshot(w.genesis().Save.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Through != (fold.Upto{Key: evs[len(evs)-1].Order()}) {
		t.Errorf("the snapshot stops at %#v; want the last event in total order", snap.Through)
	}
}

func TestGenesisIsStepsOverTheSortedEvents(t *testing.T) {
	w := generate(t, 3, 30)
	var located []fold.Located
	seen := map[events.EventID]bool{}
	for _, sc := range w.scans() {
		for _, fr := range sc.Scan.Frames {
			for _, rec := range fr.Batch.Records() {
				e, ok := events.Decode(env.Registry.Kinds(), rec).(events.Event)
				if ok && !seen[e.ID()] {
					seen[e.ID()] = true
					located = append(located, fold.Located{Event: e, Ref: sc.Range.Stream.Ref, At: store.Position{Log: sc.Range.Stream.Until.Log, At: fr.At}})
				}
			}
		}
	}
	slices.SortFunc(located, func(x, y fold.Located) int { return events.Compare(x.Event.Order(), y.Event.Order()) })
	var s fold.State
	for _, c := range fold.Check(env, located, fold.Trust{}) {
		s = fold.Step(env.Registry, s, c)
	}
	if !s.Equal(w.state()) {
		t.Errorf("stepping the sorted events disagrees with Genesis")
	}
}

// sortedEvents is every event of w, sorted by the total order.
func sortedEvents(w *world) []events.Event {
	var es []events.Event
	for _, sc := range w.scans() {
		for _, fr := range sc.Scan.Frames {
			for _, rec := range fr.Batch.Records() {
				if e, ok := events.Decode(env.Registry.Kinds(), rec).(events.Event); ok {
					es = append(es, e)
				}
			}
		}
	}
	slices.SortFunc(es, func(x, y events.Event) int { return events.Compare(x.Order(), y.Order()) })
	return es
}
