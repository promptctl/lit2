package fold_test

import (
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/fold"
)

// forger signs as its principal with bytes that do not verify.
type forger struct{ events.Signer }

func (f forger) Sign(m []byte) events.Signature {
	s := f.Signer.Sign(m)
	s.Bytes = slices.Clone(s.Bytes)
	s.Bytes[0] ^= 0xff
	return s
}

// strange signs under a suite this binary does not know.
type strange struct{ events.Signer }

func (s strange) Sign(m []byte) events.Signature {
	sig := s.Signer.Sign(m)
	sig.Suite = 9
	return sig
}

// unknown is a kind this binary's registry has no row for.
type unknown struct{}

func (unknown) Kind() events.Kind            { return "test.unknown" }
func (unknown) Schema() events.SchemaVersion { return 1 }
func (unknown) AppendTo(w *wire.Writer)      { w.Bytes(1, []byte("newer")) }

func TestAForgedSignatureIsCarriedUnappliedAndUnobserved(t *testing.T) {
	w, id := item(t)
	before := w.state()
	forged := w.mint(writer{checkout: checkoutB, signer: forger{keyB.Signer()}}, before, set(id, title, prose("forged")))[0]
	odd := w.mint(writer{checkout: checkoutA2, signer: strange{keyA.Signer()}}, before, set(id, title, prose("odd")))[0]
	s := w.state()
	carried := s.Integrity().Carried
	if len(carried) != 2 {
		t.Fatalf("carried %#v; want the forged and the unknown-suite event", carried)
	}
	want := map[events.EventID]fold.Carried{
		forged.ID(): {Event: forged.ID(), Writer: checkoutB, Principal: keyB.Signer().Principal(), RecordedAt: t0, Reason: fold.BadSignature{}},
		odd.ID():    {Event: odd.ID(), Writer: checkoutA2, Principal: keyA.Signer().Principal(), RecordedAt: t0, Reason: fold.UnknownSuite{}},
	}
	for _, c := range carried {
		w := want[c.Event]
		if c.Writer != w.Writer || c.Principal != w.Principal || !c.RecordedAt.Equal(w.RecordedAt) || c.Reason != w.Reason {
			t.Errorf("carried %#v; want %#v", c, w)
		}
	}
	if _, ok := found(t, s, id).Facts.Text(title).(fold.Unset); !ok {
		t.Errorf("a carried event applied: the title is %#v", found(t, s, id).Facts.Text(title))
	}
	if s.Frontier() != before.Frontier() {
		t.Errorf("a carried event moved the frontier from %v to %v", before.Frontier(), s.Frontier())
	}
}

func TestAnUnknownKindIsUnreadWithItsNameKept(t *testing.T) {
	w := newWorld(t)
	e := w.mint(a, w.base, events.Act{About: events.NewItem{ID: ghost("v"), Width: 3}, Payload: unknown{}})[0]
	s := w.state()
	un := s.Integrity().Unapplied
	if len(un) != 1 || un[0].Stamp.Event != e.ID() || un[0].Kind != "test.unknown" || un[0].Why != (fold.Unread{Why: events.UnknownKind{}}) {
		t.Fatalf("an unknown kind is %#v; want it Unapplied{Unread{UnknownKind}}", un)
	}
	if _, ok := s.Item(idOf(e)).(fold.NotFound); !ok {
		t.Errorf("an unknown kind made an item")
	}
	if s.Frontier() == w.base.Frontier() {
		t.Errorf("an unread event's header did not feed the frontier")
	}
	if !s.Known().Holds(idOf(e)) {
		t.Errorf("an unread create's id %s is not Known", idOf(e))
	}
	// an unread event about an id no create named makes it known
	named := ghost("y")
	w.mint(a, w.state(), events.Act{About: events.Item{ID: named}, Payload: unknown{}})
	if !w.state().Known().Holds(named) {
		t.Errorf("an unread act about %s leaves it unknown", named)
	}
}

func TestACommentAboutAnItemIsMisaddressed(t *testing.T) {
	w, id := item(t)
	e := w.mint(a, w.state(), events.Act{About: events.Item{ID: id}, Payload: events.CommentAdd{Body: events.SealPlain("lost")}})[0]
	s := w.state()
	un := s.Integrity().Unapplied
	if len(un) != 1 || un[0].Stamp.Event != e.ID() {
		t.Fatalf("a comment about an item is %#v; want it unapplied", un)
	}
	if _, ok := un[0].Why.(fold.Misaddressed); !ok {
		t.Errorf("a comment about an item is %#v; want Misaddressed", un[0].Why)
	}
	if len(found(t, s, id).Elements) != 0 {
		t.Errorf("a misaddressed comment listed on the item")
	}
}
