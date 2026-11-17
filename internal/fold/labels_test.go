package fold_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

func addLabel(id events.ItemID, l events.Name) events.Draft {
	return events.Act{About: events.Item{ID: id}, Payload: events.MemberAdd{Field: labels, Member: l}}
}

func rmLabel(id events.ItemID, l events.Name, observed ...events.Event) events.Draft {
	return events.Act{About: events.Item{ID: id}, Payload: events.MemberRm{Field: labels, Member: l, Observed: idsOf(observed)}}
}

func comment(id events.ItemID, text string) events.Draft {
	return events.CreateElement{Of: id, Payload: events.CommentAdd{Body: events.SealPlain(text)}}
}

func TestAConcurrentRemoveLeavesAFreshAdd(t *testing.T) {
	w, id := item(t)
	urgent := name("urgent")
	first := w.mint(a, w.state(), addLabel(id, urgent))[0]
	seen := w.state()
	w.mint(b, seen, rmLabel(id, urgent, first))
	fresh := w.mint(a, seen, addLabel(id, urgent))[0]
	got := found(t, w.state(), id).Facts.Members(labels)
	if len(got) != 1 || got[0].Name != urgent || !stamped(got[0].Added, fresh) {
		t.Errorf("a remove concurrent with a fresh add leaves %#v; want urgent, added by the fresh add alone", got)
	}
	w.mint(b, w.state(), rmLabel(id, urgent, fresh))
	if got := found(t, w.state(), id).Facts.Members(labels); len(got) != 0 {
		t.Errorf("a remove that observed every add leaves %#v; want none", got)
	}
}

func TestCommentsListInCreationOrder(t *testing.T) {
	w, id := item(t)
	seen := w.state()
	fromA := w.mint(a, seen, comment(id, "A says"))[0]
	fromB := w.mint(b, seen, comment(id, "B says"))[0]
	third := w.mint(a, w.state(), comment(id, "A again"))[0]
	first, second := fromA, fromB
	if later(fromA, fromB).ID() == fromA.ID() {
		first, second = fromB, fromA
	}
	els := found(t, w.state(), id).Elements
	if len(els) != 3 {
		t.Fatalf("three comments list %#v", els)
	}
	for i, e := range []events.Event{first, second, third} {
		el := els[i]
		want := e.Body().(events.Readable).Payload.(events.CommentAdd).Body.Commitment()
		text, ok := el.Facts.Text(bodyName).(fold.Agreed)
		created, isCreated := el.Origin.(fold.Created)
		if el.In != comments || el.ID != e.Header().About().(events.NewElement).ID || created.Segment != e.Header().About().(events.NewElement).Segment() || !ok || text.Body.Commitment() != want || !isCreated || created.Stamp.Event != e.ID() {
			t.Errorf("comment %d is %#v; want %s's comment, its id and segment, in comments, its body agreed", i, el, e.ID())
		}
	}
}
