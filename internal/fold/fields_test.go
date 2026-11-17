package fold_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/store"
)

// item is a world with one leaf in it, and the leaf.
func item(t *testing.T) (*world, events.ItemID) {
	w := newWorld(t)
	return w, idOf(w.mint(a, w.base, create(events.AtLevel{Level: level(1)}))[0])
}

func TestTwoProseSetsOverOnePriorAreConflictedUntilASetNamesBoth(t *testing.T) {
	w, id := item(t)
	prior := w.mint(a, w.state(), set(id, title, prose("first")))[0]
	seen := w.state()
	fromA := w.mint(a, seen, set(id, title, prose("A's"), prior))[0]
	fromB := w.mint(b, seen, set(id, title, prose("B's"), prior))[0]
	s := w.state()
	c, ok := found(t, s, id).Facts.Text(title).(fold.Conflicted)
	if !ok || len(c.Candidates) != 2 {
		t.Fatalf("two concurrent texts are %#v; want Conflicted with both", found(t, s, id).Facts.Text(title))
	}
	first, second := fromA, fromB
	if later(fromA, fromB).ID() == fromA.ID() {
		first, second = fromB, fromA
	}
	for i, e := range []events.Event{first, second} {
		want := e.Body().(events.Readable).Payload.(events.FieldSet).Value.(events.Prose).Body.Commitment()
		if c.Candidates[i].Value.(events.Prose).Body.Commitment() != want || !stamped(c.Candidates[i].By, e) {
			t.Errorf("candidate %d is %#v; want the text of %s", i, c.Candidates[i], e.ID())
		}
	}
	if !advised(s, fold.OnItem{ID: id}, fold.Conflict{Field: title}) {
		t.Errorf("a prose conflict is not on /integrity: %v", s.Advisories())
	}
	resolved := w.mint(b, s, set(id, title, prose("both, merged"), fromA, fromB))[0]
	s = w.state()
	if got, ok := found(t, s, id).Facts.Text(title).(fold.Agreed); !ok || got.Body.Commitment() != commitment("both, merged") || !stamped(got.By, resolved) {
		t.Errorf("a set naming both is %#v; want it alone", found(t, s, id).Facts.Text(title))
	}
	if len(s.Advisories()) != 0 {
		t.Errorf("the resolved field still raises %v", s.Advisories())
	}
}

func TestEqualTextsWrittenConcurrentlyAgree(t *testing.T) {
	w, id := item(t)
	seen := w.state()
	fromA := w.mint(a, seen, set(id, title, prose("same")))[0]
	fromB := w.mint(b, seen, set(id, title, prose("same")))[0]
	first, second := fromA, fromB
	if later(fromA, fromB).ID() == fromA.ID() {
		first, second = fromB, fromA
	}
	got, ok := found(t, w.state(), id).Facts.Text(title).(fold.Agreed)
	if !ok || got.Body.Commitment() != commitment("same") || !stamped(got.By, first, second) {
		t.Errorf("one text written twice is %#v; want Agreed with both stamps", found(t, w.state(), id).Facts.Text(title))
	}
}

func TestTwoConcurrentPlainValuesSettleToTheLaterAndNameTheOther(t *testing.T) {
	w, id := item(t)
	seen := w.state()
	high := w.mint(a, seen, set(id, priority, plain("high")))[0]
	low := w.mint(b, seen, set(id, priority, plain("low")))[0]
	winner, loser, lost := low, high, "high"
	if later(high, low).ID() == high.ID() {
		winner, loser, lost = high, low, "low"
	}
	s := w.state()
	got, ok := found(t, s, id).Facts.Plain(priority).(fold.Settled)
	if !ok || got.By.Event != winner.ID() || len(got.Overruled) != 1 || !isPlain(got.Overruled[0].Value, lost) || !stamped(got.Overruled[0].By, loser) {
		t.Fatalf("two concurrent values are %#v; want the later in total order, the other overruled", found(t, s, id).Facts.Plain(priority))
	}
	if !advised(s, fold.OnItem{ID: id}, fold.Conflict{Field: priority}) {
		t.Errorf("an overruled value raises no Conflict: %v", s.Advisories())
	}
	over := w.mint(a, s, set(id, priority, plain("medium"), high, low))[0]
	s = w.state()
	if got, ok := found(t, s, id).Facts.Plain(priority).(fold.Settled); !ok || got.Value != "medium" || got.By.Event != over.ID() || len(got.Overruled) != 0 || len(s.Advisories()) != 0 {
		t.Errorf("a set over both is %#v with %v; want it alone and no advisory", found(t, s, id).Facts.Plain(priority), s.Advisories())
	}
}

func TestAPlainSetThatSawTheOldValueRaisesNone(t *testing.T) {
	w, id := item(t)
	old := w.mint(a, w.state(), set(id, priority, plain("high")))[0]
	w.mint(b, w.state(), set(id, priority, plain("low"), old))
	s := w.state()
	if got, ok := found(t, s, id).Facts.Plain(priority).(fold.Settled); !ok || got.Value != "low" || len(got.Overruled) != 0 || len(s.Advisories()) != 0 {
		t.Errorf("a set over the value it saw is %#v with %v; want low and nothing overruled", found(t, s, id).Facts.Plain(priority), s.Advisories())
	}
}

func TestAHighPositionedWriteRetiresNothingItDidNotSee(t *testing.T) {
	w, id := item(t)
	high := w.mint(a, w.state(), set(id, priority, plain("high")))[0]
	// B folds A's create and never A's value, and writes enough that its
	// next write sorts far after A's
	blind := func() fold.State { return w.view(map[store.Ref]int{a.ref(): 1}) }
	for range 5 {
		w.mint(b, blind(), set(id, title, prose("busy")))
	}
	low := w.mint(b, blind(), set(id, priority, plain("low")))[0]
	if later(high, low).ID() != low.ID() {
		t.Fatalf("B's write does not sort after A's, so the test does not reach its case")
	}
	s := w.state()
	got, ok := found(t, s, id).Facts.Plain(priority).(fold.Settled)
	if !ok || got.Value != "low" || len(got.Overruled) != 1 || !isPlain(got.Overruled[0].Value, "high") || !advised(s, fold.OnItem{ID: id}, fold.Conflict{Field: priority}) {
		t.Errorf("a later write that saw nothing is %#v; want A's value overruled and named, never retired unseen", found(t, s, id).Facts.Plain(priority))
	}
}

func TestAFieldInBothFormsIsMixedUntilAWriteNamesTheProse(t *testing.T) {
	w, id := item(t)
	seen := w.state()
	text := w.mint(a, seen, set(id, priority, prose("urgent, I think")))[0]
	w.mint(b, seen, set(id, priority, plain("high")))
	s := w.state()
	if !advised(s, fold.OnItem{ID: id}, fold.MixedForms{Field: priority}) {
		t.Fatalf("a field in both forms raises %v; want MixedForms", s.Advisories())
	}
	if _, ok := found(t, s, id).Facts.Text(priority).(fold.Agreed); !ok {
		t.Errorf("the prose candidate is %#v; want it kept beside the plain one", found(t, s, id).Facts.Text(priority))
	}
	w.mint(a, s, set(id, priority, plain("urgent"), text))
	s = w.state()
	if _, ok := found(t, s, id).Facts.Text(priority).(fold.Unset); !ok || advised(s, fold.OnItem{ID: id}, fold.MixedForms{Field: priority}) {
		t.Errorf("a plain write over the prose leaves %#v and %v; want the prose retired", found(t, s, id).Facts.Text(priority), s.Advisories())
	}
}
