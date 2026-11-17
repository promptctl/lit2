package events

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/git"
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
				return m.From == MustName("backlog") && m.To == MustName("plan") && slices.Equal(m.Over, over)
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

// panicked runs f and is the message it panicked with, or "" if it returned.
func panicked(f func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg, _ = r.(string)
		}
	}()
	f()
	return ""
}

// A caller can write a zero Level, Name or Label, which no reader reads; the
// writer refuses it, so no signed event can carry it. Each refusal is matched
// by its message, and a valid value is the control that must not refuse.
func TestAZeroLevelOrNameIsRefusedByItsWriter(t *testing.T) {
	for _, c := range []struct {
		what, want string
		p          Payload
	}{
		{"a level", "is not a level", ItemCreate{Place: AtLevel{}}},
		{"a field name", "a zero Name is not a name", FieldSet{Value: Plain{Canonical: "x"}}},
		{"a member", "a zero Name is not a name", MemberAdd{Field: labels}},
	} {
		if got := panicked(func() { payloadBytes(c.p) }); !strings.Contains(got, c.want) {
			t.Errorf("%s: the writer panicked %q; want a refusal containing %q", c.what, got, c.want)
		}
	}
	if got := panicked(func() { payloadBytes(ItemCreate{Place: AtLevel{Level: mustLevel(1)}}) }); got != "" {
		t.Errorf("a valid level panicked %q; the control must write", got)
	}
}

// A zero Labeled agent reads back as Unlabeled, so the writer refuses it. A
// labeled agent is the control that must be written.
func TestAZeroAgentLabelIsRefusedByItsWriter(t *testing.T) {
	act := Act{About: Item{ID: anItem}, Payload: MemberAdd{Field: labels, Member: MustName("a")}}
	signedWith := func(a Agent) string {
		c := context(1, Frontier{}, Known{})
		c.Agent = a
		return panicked(func() { mint(t, c, keyOf(1), 1, act) })
	}
	if got := signedWith(Labeled{}); !strings.Contains(got, "reads back as Unlabeled") {
		t.Errorf("a zero Labeled agent panicked %q; want a refusal that names Unlabeled", got)
	}
	if got := signedWith(AgentFrom("x")); got != "" {
		t.Errorf("a labeled agent panicked %q; the control must sign", got)
	}
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
	// an apply's blob is git's id for the bytes it carries, or it is refused
	var otherBlob, noBlob, noVersion wire.Writer
	otherBlob.Bytes(1, aVersion)
	otherBlob.Bytes(2, objectIDWire(git.HashBlob(git.SHA1, []byte("other")), "blob"))
	noBlob.Bytes(1, aVersion)
	noVersion.Bytes(2, objectIDWire(git.HashBlob(git.SHA1, aVersion), "blob"))
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
		{"an item.create below a parent whose id is short", ReadItemCreate, place(shortParent.Encoded())},
		{"a member.add of no member", ReadMemberAdd, place([]byte("labels"))},
		{"a transition with two tos", ReadTransition, twoTos.Encoded()},
		{"a field.set with two fields", ReadFieldSet, twoFields.Encoded()},
		{"a comment.add with no body", ReadCommentAdd, nil},
		{"a workspace.apply whose blob names other bytes", ReadWorkspaceApply, otherBlob.Encoded()},
		{"a workspace.apply with no blob", ReadWorkspaceApply, noBlob.Encoded()},
		{"a workspace.apply with no version", ReadWorkspaceApply, noVersion.Encoded()},
	} {
		if _, err := c.read(wire.NewReader(c.b)); err == nil {
			t.Errorf("%s reads", c.name)
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
		if _, err := ParseName(s); err == nil {
			t.Errorf("ParseName(%q) reads", s)
		}
	}
	for _, n := range []int{0, 6, -1} {
		if _, err := ParseLevel(n); err == nil {
			t.Errorf("ParseLevel(%d) reads", n)
		}
	}
	if _, ok := mustLevel(1).Below(); ok {
		t.Error("a leaf has a level below it")
	}
	if below, ok := mustLevel(5).Below(); !ok || below != mustLevel(4) {
		t.Errorf("below level 5 is %v, %v", below, ok)
	}
}
