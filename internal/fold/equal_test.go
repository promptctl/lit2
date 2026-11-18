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
	red := w.mint(a, w.state(), addLabel(id, name("red")))[0]
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
		{"a transition", func(w *world, id events.ItemID, _, _ events.Event) {
			w.mint(a, w.state(), events.Act{About: events.Item{ID: id}, Payload: events.Transition{From: name("backlog"), To: name("plan")}})
		}},
		{"a second create", func(w *world, id events.ItemID, _, _ events.Event) {
			w.mint(b, w.base, events.Act{About: events.NewItem{ID: id, Width: 3}, Payload: events.ItemCreate{Place: events.AtLevel{Level: level(1)}}})
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
			w.mint(a, w.state(), addLabel(id, name("blue")))
		}},
		{"the label again", func(w *world, id events.ItemID, _, _ events.Event) {
			w.mint(b, w.state(), addLabel(id, name("red")))
		}},
		{"a label removed", func(w *world, id events.ItemID, _, red events.Event) {
			w.mint(a, w.state(), rmLabel(id, name("red"), red))
		}},
		{"a remove that saw nothing", func(w *world, id events.ItemID, _, _ events.Event) {
			w.mint(a, w.state(), rmLabel(id, name("red")))
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
			was := w.replay()
			c.add(w, id, named, red)
			now := w.replay()
			if now.State.Equal(was.State) || was.State.Equal(now.State) {
				t.Errorf("a state with %s is Equal to one without", c.name)
			}
		})
	}
}

func TestAReplayIsStepsOverTheSortedEvents(t *testing.T) {
	w := generate(t, 3, 30)
	var located []events.Event
	at := map[events.EventID]int{}
	for _, sc := range w.scans() {
		for _, fr := range sc.Scan.Frames {
			for _, rec := range fr.Batch.Records() {
				e, ok := events.Decode(env.Registry().Kinds(), rec).(events.Event)
				if !ok {
					continue
				}
				i, dup := at[e.ID()]
				switch {
				case !dup:
					at[e.ID()] = len(located)
					located = append(located, e)
				case fold.Rank(e.Body()) > fold.Rank(located[i].Body()):
					located[i] = e
				}
			}
		}
	}
	slices.SortFunc(located, func(x, y events.Event) int { return events.Compare(x.Order(), y.Order()) })
	if s := fold.Extend(env, fold.Seed(w.founding), located).State; !s.Equal(w.state()) {
		t.Errorf("stepping the sorted events disagrees with the replay")
	}
}
