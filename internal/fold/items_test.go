package fold_test

import (
	"math/rand/v2"
	"slices"
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
	ghost := ghost("z")
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
	if !s.Known().Holds(ghost) {
		t.Errorf("the orphan's missing parent %s is not Known", ghost)
	}
}

func TestACreateIsAboutWhatItCreatesAndNothingElse(t *testing.T) {
	w := newWorld(t)
	epic := w.mint(a, fold.State{}, create(events.AtLevel{Level: level(3)}))[0]
	recreated := w.mint(a, w.state(), events.Act{About: events.Item{ID: idOf(epic)}, Payload: events.ItemCreate{Place: events.AtLevel{Level: level(1)}}})[0]
	written := w.mint(a, w.state(), events.Act{About: events.NewItem{ID: ghost("w"), Width: 3}, Payload: events.FieldSet{Field: title, Value: prose("t")}})[0]
	s := w.state()
	un := s.Integrity().Unapplied
	if len(un) != 2 {
		t.Fatalf("unapplied %#v; want the create about an item and the write about a create", un)
	}
	for i, e := range []events.Event{recreated, written} {
		if _, ok := un[i].Why.(fold.Misaddressed); !ok || un[i].Stamp.Event != e.ID() || un[i].About != e.Header().About() {
			t.Errorf("unapplied %d is %#v; want %s Misaddressed", i, un[i], e.ID())
		}
	}
	if _, ok := found(t, s, idOf(epic)).Origin.(fold.Created); !ok {
		t.Errorf("a create about an existing item collided with it")
	}
}

func TestACreateReplayedIntoAnotherLogKeepsItsIDAndSegment(t *testing.T) {
	src := newWorld(t)
	original := src.mint(a, fold.State{}, create(events.AtLevel{Level: level(2)}))[0]
	made := original.Header().About().(events.NewItem)
	// another store, another writer, a later position: the create replayed
	dst := newWorld(t)
	dst.entropy = rand.NewChaCha8([32]byte{'d', 's', 't'})
	dst.mint(b, fold.State{}, create(events.AtLevel{Level: level(1)}), create(events.AtLevel{Level: level(1)}))
	replayed := dst.mint(b, dst.state(), events.Act{About: made, Payload: original.Body().(events.Readable).Payload})[0]
	if replayed.ID() == original.ID() || replayed.Header().Position() == original.Header().Position() {
		t.Fatal("the replay is the original event; the test replays nothing")
	}
	created, ok := found(t, dst.state(), made.ID).Origin.(fold.Created)
	if !ok || created.Segment != made.Segment() || created.In != (fold.Home{Holds: level(2)}) {
		t.Errorf("the replayed create folds to %#v; want id %s, segment %s, at level 2", created, made.ID, made.Segment())
	}
}

func TestAnItemRendersUnderItsParentsSegments(t *testing.T) {
	w := newWorld(t)
	epic := w.mint(a, fold.State{}, create(events.AtLevel{Level: level(3)}))[0]
	child := w.mint(a, w.state(), createUnder(idOf(epic)))[0]
	orphan := w.mint(a, w.state(), createUnder(ghost("z")))[0]
	s := w.state()
	seg := func(e events.Event) events.Segment { return e.Header().About().(events.NewItem).Segment() }
	for _, c := range []struct {
		name string
		id   events.ItemID
		want []events.Segment
	}{
		{"a top-level item", idOf(epic), []events.Segment{seg(epic)}},
		{"a child", idOf(child), []events.Segment{seg(epic), seg(child)}},
		{"an orphan", idOf(orphan), []events.Segment{seg(orphan)}},
		{"an id nothing created", ghost("z"), nil},
	} {
		if got := s.Path(c.id); !slices.Equal(got, c.want) {
			t.Errorf("%s renders by %v; want %v", c.name, got, c.want)
		}
	}
}

func TestAParentCreatedAfterItsChildIsNoLinkAndTheWalkEnds(t *testing.T) {
	w := newWorld(t)
	p := ghost("p")
	child := w.mint(a, fold.State{}, createUnder(p))[0]
	// a forged create of p, under its own child, sorting after the child's
	w.mint(a, w.state(), events.Act{About: events.NewItem{ID: p, Width: 3}, Payload: events.ItemCreate{Place: events.BelowParent{Parent: idOf(child)}}})
	s := w.state()
	for _, id := range []events.ItemID{idOf(child), p} {
		if got := found(t, s, id).Level; got != (fold.Orphaned{Missing: p}) {
			t.Errorf("%s in a forged cycle is %#v; want Orphaned{%s}", id, got, p)
		}
		if got := s.Path(id); len(got) == 0 {
			t.Errorf("%s in a forged cycle renders by no segments", id)
		}
	}
}

func TestTwoCreatesOfOneIDCollideAndTheFirstPlacesIt(t *testing.T) {
	w := newWorld(t)
	first := w.mint(a, fold.State{}, create(events.AtLevel{Level: level(2)}))[0]
	id := idOf(first)
	// B never folded A's create, and names the same id at its own level
	again := w.mint(b, fold.State{}, events.Act{About: events.NewItem{ID: id, Width: 3}, Payload: events.ItemCreate{Place: events.AtLevel{Level: level(1)}}})[0]
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
