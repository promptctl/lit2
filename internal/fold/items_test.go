package fold_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

func TestAParentlessLeafIsInTheHomeOfLevelOne(t *testing.T) {
	w := newWorld(t)
	leaf := idOf(w.mint(a, fold.State{}, create(events.AtLevel{Level: level(1)}))[0])
	s := w.state()
	it := found(t, s, leaf)
	created, ok := it.Origin.(fold.Created)
	if !ok || created.In != (fold.Home{Holds: level(1)}) || it.Level != (fold.Leveled{Level: level(1)}) || it.Life != (fold.Live{}) {
		t.Errorf("a parentless leaf is %#v; want created in Home{1}, Leveled{1}, live", it)
	}
	if kids := s.Children(fold.Home{Holds: level(1)}); len(kids) != 1 || kids[0] != leaf {
		t.Errorf("Home{1} holds %v; want the leaf", kids)
	}
	if len(s.Items()) != 1 || len(s.Advisories()) != 0 {
		t.Errorf("one leaf folds to %d items and advisories %v", len(s.Items()), s.Advisories())
	}
}

func TestAChildIsOneLevelBelowItsParentAndUnderIt(t *testing.T) {
	w := newWorld(t)
	epic := idOf(w.mint(a, fold.State{}, create(events.AtLevel{Level: level(3)}))[0])
	child := idOf(w.mint(a, w.state(), createUnder(epic))[0])
	second := idOf(w.mint(b, w.state(), createUnder(epic))[0])
	s := w.state()
	it := found(t, s, child)
	if it.Level != (fold.Leveled{Level: level(2)}) || it.Origin.(fold.Created).In != (fold.Under{Parent: epic}) {
		t.Errorf("a child of a level-3 item is %#v; want Leveled{2} under it", it)
	}
	if kids := s.Children(fold.Under{Parent: epic}); len(kids) != 2 || kids[0] != child || kids[1] != second {
		t.Errorf("the epic's children are %v; want %s then %s, in creation order", kids, child, second)
	}
}

func TestAChildOfALeafIsUnderLeaf(t *testing.T) {
	w := newWorld(t)
	leaf := idOf(w.mint(a, fold.State{}, create(events.AtLevel{Level: level(1)}))[0])
	child := idOf(w.mint(a, w.state(), createUnder(leaf))[0])
	grandchild := idOf(w.mint(a, w.state(), createUnder(child))[0])
	s := w.state()
	for _, id := range []events.ItemID{child, grandchild} {
		if got := found(t, s, id).Level; got != (fold.UnderLeaf{Leaf: leaf}) {
			t.Errorf("%s under a leaf is %#v; want UnderLeaf{%s}", id, got, leaf)
		}
		if !advised(s, fold.OnItem{ID: id}, fold.UnderLeaf{Leaf: leaf}) {
			t.Errorf("%s under a leaf raises no advisory: %v", id, s.Advisories())
		}
	}
}

func TestAChildOfANeverCreatedIDIsOrphaned(t *testing.T) {
	w := newWorld(t)
	ghost := must(events.ParseItemID("lit-zzz"))
	child := idOf(w.mint(a, fold.State{}, createUnder(ghost))[0])
	s := w.state()
	if got := found(t, s, child).Level; got != (fold.Orphaned{Missing: ghost}) {
		t.Errorf("a child of a never-created id is %#v; want Orphaned{%s}", got, ghost)
	}
	if !advised(s, fold.OnItem{ID: child}, fold.Orphaned{Missing: ghost}) {
		t.Errorf("the orphan raises no advisory: %v", s.Advisories())
	}
	if _, ok := s.Item(ghost).(fold.NotFound); !ok {
		t.Errorf("the missing parent is %#v; want NotFound", s.Item(ghost))
	}
	if !s.Names().Taken(top, ghost.Segments()[0]) {
		t.Errorf("the orphan's missing parent %s is free in Names", ghost)
	}
}

func TestALevelStatedOnAChildIsMisaddressed(t *testing.T) {
	w := newWorld(t)
	epic := idOf(w.mint(a, fold.State{}, create(events.AtLevel{Level: level(3)}))[0])
	stated := w.mint(a, w.state(), events.Create{In: events.Children{Parent: events.Under{ID: epic}}, Payload: events.ItemCreate{Place: events.AtLevel{Level: level(1)}}})[0]
	unstated := w.mint(a, w.state(), events.Create{In: top, Payload: events.ItemCreate{Place: events.BelowParent{}}})[0]
	s := w.state()
	for _, e := range []events.Event{stated, unstated} {
		if _, ok := s.Item(idOf(e)).(fold.NotFound); !ok {
			t.Errorf("a misplaced create made %#v", s.Item(idOf(e)))
		}
	}
	un := s.Integrity().Unapplied
	if len(un) != 2 {
		t.Fatalf("unapplied %#v; want the two misplaced creates", un)
	}
	for i, e := range []events.Event{stated, unstated} {
		if _, ok := un[i].Why.(fold.Misaddressed); !ok || un[i].Stamp.Event != e.ID() || un[i].About != e.Header().About() {
			t.Errorf("unapplied %d is %#v; want %s Misaddressed", i, un[i], idOf(e))
		}
	}
}

func TestTwoCreatesOfOneIDCollideAndTheFirstPlacesIt(t *testing.T) {
	w := newWorld(t)
	first := w.mint(a, fold.State{}, create(events.AtLevel{Level: level(2)}))[0]
	id := idOf(first)
	// B never folded A's create, and names the same id at its own level
	again := w.mint(b, fold.State{}, events.Act{About: events.Item{ID: id}, Payload: events.ItemCreate{Place: events.AtLevel{Level: level(1)}}})[0]
	s := w.state()
	it := found(t, s, id)
	c, ok := it.Origin.(fold.Collided)
	firstInOrder, other := first, again
	if later(first, again).ID() == first.ID() {
		firstInOrder, other = again, first
	}
	if !ok || c.First.Stamp.Event != firstInOrder.ID() || !stamped(c.Others, other) {
		t.Fatalf("two creates of %s are %#v; want Collided, the first in total order its creator", id, it.Origin)
	}
	want := fold.Home{Holds: level(2)}
	if firstInOrder.ID() == again.ID() {
		want = fold.Home{Holds: level(1)}
	}
	if c.First.In != want || it.Level != (fold.Leveled{Level: want.Holds}) {
		t.Errorf("the collided item is in %#v at %#v; want the first create's %#v", c.First.In, it.Level, want)
	}
	if !advised(s, fold.OnItem{ID: id}, c) || s.Children(want)[0] != id {
		t.Errorf("the collision is not on /integrity or the item is not in its home: %v", s.Advisories())
	}
}

func TestADeletedItemIsTombstonedAndKeepsEveryFact(t *testing.T) {
	w := newWorld(t)
	id := idOf(w.mint(a, fold.State{}, create(events.AtLevel{Level: level(1)}))[0])
	named := w.mint(a, w.state(), set(id, title, prose("kept")))[0]
	del := w.mint(a, w.state(), events.Act{About: events.Item{ID: id}, Payload: events.ItemDelete{}})[0]
	w.mint(b, w.state(), events.Act{About: events.Item{ID: id}, Payload: events.ItemDelete{}})
	it := found(t, w.state(), id)
	if tomb, ok := it.Life.(fold.Tombstoned); !ok || tomb.By.Event != del.ID() {
		t.Errorf("a deleted item's life is %#v; want Tombstoned by the first delete", it.Life)
	}
	if got, ok := it.Facts.Text(title).(fold.Agreed); !ok || got.Body.Commitment() != commitment("kept") || !stamped(got.By, named) {
		t.Errorf("a deleted item's title is %#v; want it kept", it.Facts.Text(title))
	}
}
