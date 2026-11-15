package events

import (
	"bytes"
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/events/wire"
)

// first is the first facts' decode table.
var first = NewKinds(Row[ItemCreate](ReadItemCreate), Row[ItemDelete](ReadItemDelete), Row[FieldSet](ReadFieldSet),
	Row[LabelAdd](ReadLabelAdd), Row[LabelRm](ReadLabelRm), Row[CommentAdd](ReadCommentAdd))

func mustName(s string) Name {
	n, err := ParseName(s)
	if err != nil {
		panic(err)
	}
	return n
}

func mustLabel(s string) Label {
	l, err := ParseLabel(s)
	if err != nil {
		panic(err)
	}
	return l
}

func mustLevel(n int) Level {
	l, err := ParseLevel(n)
	if err != nil {
		panic(err)
	}
	return l
}

func payloadBytes(p Payload) []byte {
	var w wire.Writer
	p.AppendTo(&w)
	return w.Encoded()
}

func TestTheFirstFactsReadBackAsMinted(t *testing.T) {
	k := keyOf(1)
	id := Top{Prefix: prefix}.Child(Segment{s: "abc"})
	over := []EventID{{1}, {2}}
	for _, c := range []struct {
		draft Draft
		same  func(Payload) bool
	}{
		{Create{In: Children{Parent: Top{Prefix: prefix}}, Payload: ItemCreate{Place: AtLevel{Level: mustLevel(3)}}},
			func(p Payload) bool {
				at, ok := p.(ItemCreate).Place.(AtLevel)
				return ok && at.Level == mustLevel(3)
			}},
		{Create{In: Children{Parent: Under{ID: id}}, Payload: ItemCreate{Place: BelowParent{}}},
			func(p Payload) bool { _, ok := p.(ItemCreate).Place.(BelowParent); return ok }},
		{Act{About: Item{ID: id}, Payload: ItemDelete{}},
			func(p Payload) bool { _, ok := p.(ItemDelete); return ok }},
		{Act{About: Item{ID: id}, Payload: FieldSet{Field: mustName("title"), Value: Prose{Body: text("a title")}, Over: over}},
			func(p Payload) bool {
				f := p.(FieldSet)
				pr, ok := f.Value.(Prose)
				return f.Field == mustName("title") && ok && pr.Body.Commitment() == text("a title").Commitment() && slices.Equal(f.Over, over)
			}},
		{Act{About: Item{ID: id}, Payload: FieldSet{Field: mustName("priority"), Value: Plain{Canonical: "high"}}},
			func(p Payload) bool {
				f := p.(FieldSet)
				pl, ok := f.Value.(Plain)
				return f.Field == mustName("priority") && ok && pl.Canonical == "high" && len(f.Over) == 0
			}},
		{Act{About: Item{ID: id}, Payload: LabelAdd{Label: mustLabel("urgent")}},
			func(p Payload) bool { return p.(LabelAdd).Label == mustLabel("urgent") }},
		{Act{About: Item{ID: id}, Payload: LabelRm{Label: mustLabel("urgent"), Observed: over}},
			func(p Payload) bool {
				r := p.(LabelRm)
				return r.Label == mustLabel("urgent") && slices.Equal(r.Observed, over)
			}},
		{Create{In: Elements{Of: id}, Payload: CommentAdd{Body: text("a comment")}},
			func(p Payload) bool { return p.(CommentAdd).Body.Commitment() == text("a comment").Commitment() }},
	} {
		m := mint(t, context(1, Frontier{}, Names{}), k, 1, c.draft)
		e := decoded(t, first, records(m)[0])
		read, ok := e.Body().(Readable)
		if !ok {
			t.Errorf("a minted %T reads %#v", c.draft, e.Body())
			continue
		}
		if !c.same(read.Payload) {
			t.Errorf("a minted %s reads back as %#v", read.Payload.Kind(), read.Payload)
		}
	}
}

func TestTheFirstFactsKeepTheFieldsTheyDoNotKnow(t *testing.T) {
	future := futureField(15, "newer")
	for _, c := range []struct {
		p    Payload
		read func(*wire.Reader) (Versioned, error)
	}{
		{ItemCreate{Place: AtLevel{Level: mustLevel(1)}}, ReadItemCreate},
		{ItemDelete{}, ReadItemDelete},
		{FieldSet{Field: mustName("title"), Value: Plain{Canonical: "x"}}, ReadFieldSet},
		{LabelAdd{Label: mustLabel("a")}, ReadLabelAdd},
		{LabelRm{Label: mustLabel("a")}, ReadLabelRm},
		{CommentAdd{Body: text("c")}, ReadCommentAdd},
	} {
		v, err := c.read(wire.NewReader(append(payloadBytes(c.p), future...)))
		if err != nil {
			t.Errorf("%s with a newer field: %v", c.p.Kind(), err)
			continue
		}
		if back := payloadBytes(v.(Payload)); !bytes.HasSuffix(back, future) || !bytes.Equal(back, append(payloadBytes(c.p), future...)) {
			t.Errorf("%s writes back %x; want its fields and the newer one", c.p.Kind(), back)
		}
	}
}

// A nested message keeps the fields it does not know too: a placement and a
// value written by a newer binary read and write back whole, with the
// newer field where the newer binary put it.
func TestNestedMessagesKeepTheFieldsTheyDoNotKnow(t *testing.T) {
	future := futureField(15, "newer")
	placement := append(PlacementWire(AtLevel{Level: mustLevel(2)}), future...)
	p, err := ReadPlacement(placement)
	if err != nil {
		t.Fatalf("a placement with a newer field: %v", err)
	}
	if back := PlacementWire(p); !bytes.Equal(back, placement) {
		t.Errorf("a placement writes back %x; want %x", back, placement)
	}
	for _, stored := range [][]byte{
		append(Plain{Canonical: "high"}.wire(), future...),
		append(Prose{Body: text("a title")}.wire(), future...),
	} {
		v, err := readValue(stored)
		if err != nil {
			t.Fatalf("a value with a newer field: %v", err)
		}
		if back := v.wire(); !bytes.Equal(back, stored) {
			t.Errorf("a %T writes back %x; want %x", v, back, stored)
		}
	}
}

// A caller can write a zero Level, Name or Label, which no reader reads; the
// writer refuses it, so no signed event can carry it.
func TestAZeroLevelOrNameIsRefusedByItsWriter(t *testing.T) {
	for _, c := range []struct {
		what string
		p    Payload
	}{
		{"a level", ItemCreate{Place: AtLevel{}}},
		{"a field name", FieldSet{Value: Plain{Canonical: "x"}}},
		{"a label", LabelAdd{}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: the writer wrote a zero value; want it refused", c.what)
				}
			}()
			payloadBytes(c.p)
		}()
	}
}

// A zero Labeled agent reads back as Unlabeled, so the writer refuses it.
func TestAZeroAgentLabelIsRefusedByItsWriter(t *testing.T) {
	c := context(1, Frontier{}, Names{})
	c.Agent = Labeled{}
	defer func() {
		if recover() == nil {
			t.Errorf("a zero Labeled agent was signed; want it refused")
		}
	}()
	mint(t, c, keyOf(1), 1, Act{About: Item{ID: Top{Prefix: prefix}.Child(Segment{s: "abc"})}, Payload: LabelAdd{Label: mustLabel("a")}})
}

func TestAPayloadMissingWhatItCannotBeWithoutIsRefused(t *testing.T) {
	var twoForms wire.Writer
	twoForms.Bytes(valueProse, text("a").wire())
	twoForms.Bytes(valuePlain, []byte("b"))
	if _, err := readValue(twoForms.Encoded()); err == nil {
		t.Error("a value in both forms reads")
	}
	if _, err := readValue(nil); err == nil {
		t.Error("a value in neither form reads")
	}
	var noValue wire.Writer
	noValue.Bytes(1, []byte("title"))
	var badField wire.Writer
	badField.Bytes(1, []byte("Title"))
	badField.Bytes(2, Plain{Canonical: "x"}.wire())
	var levelSix, bothPlaces wire.Writer
	levelSix.Uvarint(placeAtLevel, 6)
	bothPlaces.Uvarint(placeAtLevel, 1)
	bothPlaces.Bytes(placeBelowParent, nil)
	place := func(b []byte) []byte {
		var w wire.Writer
		w.Bytes(1, b)
		return w.Encoded()
	}
	for _, c := range []struct {
		name string
		read func(*wire.Reader) (Versioned, error)
		b    []byte
	}{
		{"a field.set with no value", ReadFieldSet, noValue.Encoded()},
		{"a field.set of a field that is no name", ReadFieldSet, badField.Encoded()},
		{"an item.create placed nowhere", ReadItemCreate, nil},
		{"an item.create at level 6", ReadItemCreate, place(levelSix.Encoded())},
		{"an item.create at a level and below its parent", ReadItemCreate, place(bothPlaces.Encoded())},
		{"a label.add of no label", ReadLabelAdd, nil},
		{"a comment.add with no body", ReadCommentAdd, nil},
	} {
		if _, err := c.read(wire.NewReader(c.b)); err == nil {
			t.Errorf("%s reads", c.name)
		}
	}
}

func TestNamesLabelsAndLevelsParseOnlyTheirOwnShape(t *testing.T) {
	for _, s := range []string{"title", "a", "due-date", "x_1"} {
		if n, err := ParseName(s); err != nil || n.String() != s {
			t.Errorf("ParseName(%q) = %v, %v", s, n, err)
		}
	}
	for _, s := range []string{"", "Title", "1a", "-a", "a b", "a.b", "a/b"} {
		if _, err := ParseName(s); err == nil {
			t.Errorf("ParseName(%q) reads", s)
		}
		if _, err := ParseLabel(s); err == nil {
			t.Errorf("ParseLabel(%q) reads", s)
		}
	}
	if l := mustLabel("urgent"); l.Name() != mustName("urgent") || l.String() != "urgent" {
		t.Errorf("a label's name is %v", l.Name())
	}
	for _, n := range []int{0, 6, -1} {
		if _, err := ParseLevel(n); err == nil {
			t.Errorf("ParseLevel(%d) reads", n)
		}
	}
	if _, ok := mustLevel(1).Below(); ok {
		t.Error("a leaf has a level below it")
	}
	if below, ok := mustLevel(5).Below(); !ok || below.Number() != 4 {
		t.Errorf("below level 5 is %v, %v", below, ok)
	}
}
