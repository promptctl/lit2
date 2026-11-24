package events

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/panics"
)

// first is the first facts' decode table.
var first = NewKinds(Row[ItemCreate](ReadItemCreate), Row[FieldSet](ReadFieldSet), Row[MemberAdd](ReadMemberAdd),
	Row[MemberRm](ReadMemberRm), Row[Transition](ReadTransition), Row[CommentAdd](ReadCommentAdd),
	Row[WorkspaceApply](ReadWorkspaceApply))

// aVersion is a workspace version's bytes, which no payload reads.
var aVersion = []byte("prefix = \"lit\"\n")

var labels = MustName("labels")

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
	id := anItem
	over := []EventID{{1}, {2}}
	for _, c := range []struct {
		draft Draft
		same  func(Payload) bool
	}{
		{CreateItem{Payload: ItemCreate{Place: AtLevel{Level: mustLevel(3)}}},
			func(p Payload) bool {
				at, ok := p.(ItemCreate).Place.(AtLevel)
				return ok && at.Level == mustLevel(3)
			}},
		{CreateItem{Payload: ItemCreate{Place: BelowParent{Parent: id}}},
			func(p Payload) bool { below, ok := p.(ItemCreate).Place.(BelowParent); return ok && below.Parent == id }},
		{Act{About: Item{ID: id}, Payload: Transition{From: MustName("backlog"), To: MustName("plan"), Over: over}},
			func(p Payload) bool {
				m := p.(Transition)
				return m.From == MustName("backlog") && m.To == MustName("plan") && slices.Equal(m.Over, over) && len(m.Fields) == 0
			}},
		{Act{About: Item{ID: id}, Payload: Transition{From: MustName("plan"), To: MustName("dup"), Fields: []TransitionField{
			{Field: MustName("of"), Value: Plain{Canonical: "lit-abc"}},
			{Field: MustName("reason"), Value: Prose{Body: text("same work")}},
		}}},
			func(p Payload) bool {
				f := p.(Transition).Fields
				of, plain := f[0].Value.(Plain)
				why, prose := f[1].Value.(Prose)
				return len(f) == 2 && f[0].Field == MustName("of") && plain && of.Canonical == "lit-abc" &&
					f[1].Field == MustName("reason") && prose && why.Body.Commitment() == text("same work").Commitment()
			}},
		{Act{About: Item{ID: id}, Payload: FieldSet{Field: MustName("title"), Value: Prose{Body: text("a title")}, Over: over}},
			func(p Payload) bool {
				f := p.(FieldSet)
				pr, ok := f.Value.(Prose)
				return f.Field == MustName("title") && ok && pr.Body.Commitment() == text("a title").Commitment() && slices.Equal(f.Over, over)
			}},
		{Act{About: Item{ID: id}, Payload: FieldSet{Field: MustName("priority"), Value: Plain{Canonical: "high"}}},
			func(p Payload) bool {
				f := p.(FieldSet)
				pl, ok := f.Value.(Plain)
				return f.Field == MustName("priority") && ok && pl.Canonical == "high" && len(f.Over) == 0
			}},
		{Act{About: Item{ID: id}, Payload: MemberAdd{Field: labels, Member: MustName("urgent")}},
			func(p Payload) bool { a := p.(MemberAdd); return a.Field == labels && a.Member == MustName("urgent") }},
		{Act{About: Item{ID: id}, Payload: MemberRm{Field: labels, Member: MustName("urgent"), Observed: over}},
			func(p Payload) bool {
				r := p.(MemberRm)
				return r.Field == labels && r.Member == MustName("urgent") && slices.Equal(r.Observed, over)
			}},
		{CreateElement{Of: id, Payload: CommentAdd{Body: text("a comment")}},
			func(p Payload) bool { return p.(CommentAdd).Body.Commitment() == text("a comment").Commitment() }},
		{Act{About: Root{}, Payload: NewWorkspaceApply(aVersion, git.SHA256, over)},
			func(p Payload) bool {
				a := p.(WorkspaceApply)
				return bytes.Equal(a.Version(), aVersion) && a.Blob() == git.HashBlob(git.SHA256, aVersion) && slices.Equal(a.Over, over)
			}},
	} {
		m := mint(t, context(1, Frontier{}, Known{}), k, 1, c.draft)
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

// A create's first facts are events about the item it names, at the
// positions after it, before the command's next draft.
func TestACreatesFactsFollowItAboutTheItemItNames(t *testing.T) {
	title := FieldSet{Field: MustName("title"), Value: Plain{Canonical: "t"}}
	m := mint(t, context(1, Frontier{}, Known{}), keyOf(1), 1,
		CreateItem{Payload: ItemCreate{Place: AtLevel{Level: mustLevel(1)}}, Then: []Payload{title, title}},
		Act{About: Item{ID: anItem}, Payload: title})
	if len(m.Events) != 4 {
		t.Fatalf("a create with two facts and an act minted %d events; want 4", len(m.Events))
	}
	made := m.Events[0].Header().About().(NewItem)
	for i, e := range m.Events {
		if e.Header().Position().n != m.Events[0].Header().Position().n+uint64(i) {
			t.Errorf("event %d is at %v; want %d after the create", i, e.Header().Position(), i)
		}
	}
	for _, e := range m.Events[1:3] {
		if e.Header().About() != (Item{ID: made.ID}) {
			t.Errorf("a create's fact is about %#v; want the item it created, %s", e.Header().About(), made.ID)
		}
	}
	if m.Events[3].Header().About() != (Item{ID: anItem}) {
		t.Errorf("the act after a create is about %#v; want its own item", m.Events[3].Header().About())
	}
}

func TestTheFirstFactsKeepTheFieldsTheyDoNotKnow(t *testing.T) {
	future := futureField(15, "newer")
	for _, c := range []struct {
		p    Payload
		read func(*wire.Reader) (Versioned, error)
	}{
		{ItemCreate{Place: AtLevel{Level: mustLevel(1)}}, ReadItemCreate},
		{FieldSet{Field: MustName("title"), Value: Plain{Canonical: "x"}}, ReadFieldSet},
		{MemberAdd{Field: labels, Member: MustName("a")}, ReadMemberAdd},
		{MemberRm{Field: labels, Member: MustName("a")}, ReadMemberRm},
		{Transition{From: MustName("a"), To: MustName("b")}, ReadTransition},
		{CommentAdd{Body: text("c")}, ReadCommentAdd},
		{NewWorkspaceApply(aVersion, git.SHA1, []EventID{{1}}), ReadWorkspaceApply},
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
	carried := append(TransitionField{Field: MustName("reason"), Value: Plain{Canonical: "x"}}.wire(), future...)
	f, err := readTransitionField(carried)
	if err != nil {
		t.Fatalf("a transition's field with a newer field: %v", err)
	}
	if back := f.wire(); !bytes.Equal(back, carried) {
		t.Errorf("a transition's field writes back %x; want %x", back, carried)
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
// writer refuses it, so no signed event can carry it. A valid value is the
// control that must write.
func TestAZeroLevelOrNameIsRefusedByItsWriter(t *testing.T) {
	for _, c := range []struct {
		what       string
		want       error
		p, control Payload
	}{
		{"a level", ErrNotALevel, ItemCreate{Place: AtLevel{}}, ItemCreate{Place: AtLevel{Level: mustLevel(1)}}},
		{"a field name", ErrNotAName, FieldSet{Value: Plain{Canonical: "x"}}, FieldSet{Field: labels, Value: Plain{Canonical: "x"}}},
		{"a member", ErrNotAName, MemberAdd{Field: labels}, MemberAdd{Field: labels, Member: MustName("a")}},
	} {
		t.Run(c.what, func(t *testing.T) {
			payloadBytes(c.control)
			panics.With(t, c.want, func() { payloadBytes(c.p) })
		})
	}
}

// A zero Labeled agent reads back as Unlabeled, so the writer refuses it. A
// labeled agent is the control that must be written.
func TestAZeroAgentLabelIsRefusedByItsWriter(t *testing.T) {
	act := Act{About: Item{ID: anItem}, Payload: MemberAdd{Field: labels, Member: MustName("a")}}
	signedWith := func(a Agent) Context {
		c := context(1, Frontier{}, Known{})
		c.Agent = a
		return c
	}
	mint(t, signedWith(AgentFrom("x")), keyOf(1), 1, act)
	panics.With(t, errEmptyLabel, func() { mint(t, signedWith(Labeled{}), keyOf(1), 1, act) })
}

func TestAPayloadMissingWhatItCannotBeWithoutIsRefused(t *testing.T) {
	var twoForms wire.Writer
	twoForms.Bytes(valueProse, text("a").wire())
	twoForms.Bytes(valuePlain, []byte("b"))
	if _, err := readValue(Plain{Canonical: "b"}.wire()); err != nil {
		t.Fatalf("a value in one form: %v; the control must read", err)
	}
	if _, err := readValue(twoForms.Encoded()); !errors.Is(err, notOneForm{What: "a value", N: 2}) {
		t.Errorf("a value in both forms reads with %v; want a value in 2 forms", err)
	}
	if _, err := readValue(nil); !errors.Is(err, notOneForm{What: "a value", N: 0}) {
		t.Errorf("a value in neither form reads with %v; want a value in 0 forms", err)
	}
	var noValue wire.Writer
	noValue.Bytes(1, []byte("title"))
	var badField wire.Writer
	badField.Bytes(1, []byte("Title"))
	badField.Bytes(2, Plain{Canonical: "x"}.wire())
	var levelSix, bothPlaces, shortParent wire.Writer
	levelSix.Uvarint(placeAtLevel, 6)
	bothPlaces.Uvarint(placeAtLevel, 1)
	bothPlaces.Bytes(placeBelowParent, anItem.b[:])
	shortParent.Bytes(placeBelowParent, anItem.b[:8])
	// a name field is one name: a second is refused, not read as either
	var twoTos, twoFields wire.Writer
	twoTos.Bytes(1, []byte("backlog"))
	twoTos.Bytes(2, []byte("plan"))
	twoTos.Bytes(2, []byte("done"))
	twoFields.Bytes(1, []byte("title"))
	twoFields.Bytes(1, []byte("reason"))
	twoFields.Bytes(2, Plain{Canonical: "x"}.wire())
	place := func(b []byte) []byte {
		var w wire.Writer
		w.Bytes(1, b)
		return w.Encoded()
	}
	// a move says one thing about each field it carries, in name order, and says it
	reason := TransitionField{Field: MustName("reason"), Value: Plain{Canonical: "x"}}.wire()
	of := TransitionField{Field: MustName("of"), Value: Plain{Canonical: "x"}}.wire()
	var twiceCarried, outOfOrder, noCarriedValue, carriedNoValue wire.Writer
	for _, w := range []*wire.Writer{&twiceCarried, &outOfOrder, &noCarriedValue} {
		w.Bytes(1, []byte("plan"))
		w.Bytes(2, []byte("obsolete"))
	}
	twiceCarried.Bytes(4, reason)
	twiceCarried.Bytes(4, reason)
	outOfOrder.Bytes(4, reason)
	outOfOrder.Bytes(4, of)
	carriedNoValue.Bytes(1, []byte("reason"))
	noCarriedValue.Bytes(4, carriedNoValue.Encoded())
	// an apply's blob is git's id for the bytes it carries, or it is refused
	var otherBlob, noBlob, noVersion wire.Writer
	otherBlob.Bytes(1, aVersion)
	otherBlob.Bytes(2, objectIDWire(git.HashBlob(git.SHA1, []byte("other")), "blob"))
	noBlob.Bytes(1, aVersion)
	noVersion.Bytes(2, objectIDWire(git.HashBlob(git.SHA1, aVersion), "blob"))
	// one version and one blob: a second of either is refused, not read as one
	var twoVersions, twoBlobs wire.Writer
	twoVersions.Bytes(1, []byte("other"))
	twoVersions.Bytes(1, aVersion)
	twoVersions.Bytes(2, objectIDWire(git.HashBlob(git.SHA1, aVersion), "blob"))
	twoBlobs.Bytes(1, aVersion)
	twoBlobs.Bytes(2, objectIDWire(git.HashBlob(git.SHA1, aVersion), "blob"))
	twoBlobs.Bytes(2, objectIDWire(git.HashBlob(git.SHA1, aVersion), "blob"))
	// the controls: each reader reads a payload of its kind whole
	for _, p := range []Payload{
		FieldSet{Field: MustName("title"), Value: Plain{Canonical: "x"}},
		ItemCreate{Place: BelowParent{Parent: anItem}},
		MemberAdd{Field: labels, Member: MustName("a")},
		Transition{From: MustName("plan"), To: MustName("obsolete"), Fields: []TransitionField{
			{Field: MustName("of"), Value: Plain{Canonical: "x"}}, {Field: MustName("reason"), Value: Plain{Canonical: "x"}}}},
		CommentAdd{Body: text("c")},
		NewWorkspaceApply(aVersion, git.SHA1, nil),
	} {
		if _, err := first.rows[p.Kind()].decode[0](wire.NewReader(payloadBytes(p))); err != nil {
			t.Fatalf("a whole %s: %v; the control must read", p.Kind(), err)
		}
	}
	for _, c := range []struct {
		name string
		read func(*wire.Reader) (Versioned, error)
		b    []byte
		want error
	}{
		{"a field.set with no value", ReadFieldSet, noValue.Encoded(), missing{What: "value"}},
		{"a field.set of a field that is no name", ReadFieldSet, badField.Encoded(), ErrNotAName},
		{"an item.create placed nowhere", ReadItemCreate, nil, missing{What: "placement"}},
		{"an item.create at level 6", ReadItemCreate, place(levelSix.Encoded()), ErrNotALevel},
		{"an item.create at a level and below its parent", ReadItemCreate, place(bothPlaces.Encoded()), notOneForm{What: "a placement", N: 2}},
		{"an item.create below a parent whose id is short", ReadItemCreate, place(shortParent.Encoded()), wrongSize{What: "an id", Got: 8, Want: 16}},
		{"a member.add of no member", ReadMemberAdd, place([]byte("labels")), missing{What: "member"}},
		{"a transition with two tos", ReadTransition, twoTos.Encoded(), twice{What: "its to"}},
		{"a transition carrying one field twice", ReadTransition, twiceCarried.Encoded(), errFieldOrder},
		{"a transition carrying its fields out of name order", ReadTransition, outOfOrder.Encoded(), errFieldOrder},
		{"a transition carrying a field with no value", ReadTransition, noCarriedValue.Encoded(), missing{What: "value"}},
		{"a field.set with two fields", ReadFieldSet, twoFields.Encoded(), twice{What: "its field"}},
		{"a comment.add with no body", ReadCommentAdd, nil, missing{What: "body"}},
		{"a workspace.apply whose blob names other bytes", ReadWorkspaceApply, otherBlob.Encoded(), errBlobNotVersion},
		{"a workspace.apply with no blob", ReadWorkspaceApply, noBlob.Encoded(), missing{What: "blob"}},
		{"a workspace.apply with no version", ReadWorkspaceApply, noVersion.Encoded(), missing{What: "version"}},
		{"a workspace.apply with two versions", ReadWorkspaceApply, twoVersions.Encoded(), twice{What: "its version"}},
		{"a workspace.apply with two blobs", ReadWorkspaceApply, twoBlobs.Encoded(), twice{What: "its blob"}},
	} {
		if _, err := c.read(wire.NewReader(c.b)); !errors.Is(err, c.want) {
			t.Errorf("%s reads with %v; want %v", c.name, err, c.want)
		}
	}
}

func TestNamesAndLevelsParseOnlyTheirOwnShape(t *testing.T) {
	for _, s := range []string{"title", "a", "due-date", "x_1"} {
		if n, err := ParseName(s); err != nil || n.String() != s {
			t.Errorf("ParseName(%q) = %v, %v", s, n, err)
		}
	}
	for _, s := range []string{"", "Title", "1a", "-a", "a b", "a.b", "a/b"} {
		if _, err := ParseName(s); !errors.Is(err, ErrNotAName) || !strings.Contains(err.Error(), "a lowercase letter, then lowercase letters, digits, _ or -") {
			t.Errorf("ParseName(%q) reads with %v; want ErrNotAName, saying a name's grammar", s, err)
		}
	}
	for n := range 5 {
		if l, err := ParseLevel(n + 1); err != nil || l.Int() != n+1 {
			t.Errorf("ParseLevel(%d) = %v, %v; the control must read", n+1, l, err)
		}
	}
	for _, n := range []int{0, 6, -1} {
		if _, err := ParseLevel(n); !errors.Is(err, ErrNotALevel) || !strings.Contains(err.Error(), "1 to 5") {
			t.Errorf("ParseLevel(%d) reads with %v; want ErrNotALevel, saying the levels are 1 to 5", n, err)
		}
	}
	if _, ok := mustLevel(1).Below(); ok {
		t.Error("a leaf has a level below it")
	}
	if below, ok := mustLevel(5).Below(); !ok || below != mustLevel(4) {
		t.Errorf("below level 5 is %v, %v", below, ok)
	}
}
