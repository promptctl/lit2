package events

import (
	"bytes"
	"io"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/events/wire"
)

func TestAFullIDRoundTripsAndItsSegmentsAreItsPrefixes(t *testing.T) {
	for _, b := range [][16]byte{{}, {0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, anItem.b} {
		id := ItemID{b: b}
		s := id.String()
		back, err := ParseItemID(s)
		if err != nil || back != id || len(s) != fullWidth {
			t.Errorf("%x renders %q, which reads back as %v, %v", b, s, back, err)
		}
		for w := minWidth; w <= fullWidth; w++ {
			if seg := id.Segment(w).String(); seg != s[:w] {
				t.Errorf("%s's %d-digit segment is %q; want its first %d digits", s, w, seg, w)
			}
		}
	}
	full := anItem.String()
	for _, s := range []string{"", full[:25], full + "0", strings.ToUpper(full), full[:25] + "1", full[:25] + "i", "lit-" + full} {
		if id, err := ParseItemID(s); err == nil {
			t.Errorf("ParseItemID(%q) = %v; want it refused", s, id)
		}
	}
}

func TestWidthGrowsWithTheNamespaceAndKeepsAMatchUnlikely(t *testing.T) {
	prev := Width(0)
	for _, fill := range append(rangeTo(1<<16), 1<<20, 1<<24, 1<<30, 1<<40) {
		w := Width(fill)
		room := uint64(1) << (5 * w)
		if w < prev || w < 3 || 1024*(uint64(fill)+1) > room {
			t.Fatalf("Width(%d) = %d after %d; want it monotone, at least 3, and (fill+1)/32^w ≤ 1/1024", fill, w, prev)
		}
		if w > 3 && 1024*(uint64(fill)+1) <= room>>5 {
			t.Fatalf("Width(%d) = %d, though %d digits would do", fill, w, w-1)
		}
		prev = w
	}
	for fill, want := range map[int]int{0: 3, 31: 3, 32: 4, 1023: 4, 1024: 5, 32767: 5, math.MaxInt: 15} {
		if got := Width(fill); got != want {
			t.Errorf("Width(%d) = %d; want %d", fill, got, want)
		}
	}
}

func rangeTo(n int) []int {
	ns := make([]int, n)
	for i := range ns {
		ns[i] = i
	}
	return ns
}

var topCreate = CreateItem{Payload: ItemCreate{Place: AtLevel{Level: mustLevel(1)}}}

func newItemOf(e Event) NewItem { return e.Header().About().(NewItem) }

func TestAnIDIsTheEntropysAndNothingElses(t *testing.T) {
	at := Frontier{highest: 7}
	// one entropy, two writers, two frontiers: one id, so where the create
	// lands is no part of it
	a := newItemOf(mint(t, context(1, at, Known{}), keyOf(1), 1, topCreate).Events[0])
	b := newItemOf(mint(t, context(2, Frontier{highest: 90}, Known{}), keyOf(2), 1, topCreate).Events[0])
	if a != b {
		t.Errorf("one entropy minted %v and %v; want one id", a, b)
	}
	// two offline writers at one frontier, each with its own entropy
	c := newItemOf(mint(t, context(1, at, Known{}), keyOf(1), 2, topCreate).Events[0])
	if a.ID == c.ID {
		t.Errorf("two entropies minted one id, %s", a.ID)
	}
}

// fixed is an entropy source that gives its bytes in order, then zeros.
type fixed struct{ b []byte }

func (f *fixed) Read(p []byte) (int, error) {
	n := copy(p, f.b)
	f.b = f.b[n:]
	clear(p[n:])
	return len(p), nil
}

// drawing is entropy whose first id is id: the id, then a zero salt.
func drawing(ids ...ItemID) *fixed {
	var b []byte
	for _, id := range ids {
		b = append(b, id.b[:]...)
		b = append(b, make([]byte, len(Salt{}))...)
	}
	return &fixed{b: b}
}

// sharing is an id that shares exactly k leading digits with id.
func sharing(id ItemID, k int) ItemID {
	out := id.b
	bit := 5 * k // the first bit of digit k
	out[bit/8] ^= 0x80 >> (bit % 8)
	return ItemID{b: out}
}

func TestASegmentIsWiderThanAnyKnownIDsSharedDigits(t *testing.T) {
	drawn := ItemID{b: [16]byte{0x5a, 0x17, 0xc3, 0x9e, 0x42, 0x0d, 0x88, 0x61}}
	for _, k := range []int{0, 2, 3, 5, 25} {
		var b KnownBuilder
		b.Saw(sharing(drawn, k))
		m, err := Mint(context(1, Frontier{}, b.Known()), NewDrafts(topCreate), keyOf(1).Signer(), drawing(drawn))
		if err != nil {
			t.Fatal(err)
		}
		got := newItemOf(m.Events[0])
		if got.ID != drawn || got.Width != max(3, k+1) {
			t.Errorf("a known id sharing %d digits gave %v; want %s at width %d", k, got, drawn, max(3, k+1))
		}
		if seg := got.Segment(); sharing(drawn, k).Segment(got.Width) == seg {
			t.Errorf("the segment %s is a known id's own prefix", seg)
		}
	}
	// a full namespace widens every segment, whatever the ids share
	var b KnownBuilder
	for range 32 {
		b.Created(Top{})
	}
	m, err := Mint(context(1, Frontier{}, b.Known()), NewDrafts(topCreate), keyOf(1).Signer(), drawing(drawn))
	if err != nil {
		t.Fatal(err)
	}
	if w := newItemOf(m.Events[0]).Width; w != 4 {
		t.Errorf("a namespace of 32 gave width %d; want 4", w)
	}
}

func TestTwoCreatesOfOneCommandWidenEachOthersSegments(t *testing.T) {
	first := ItemID{b: [16]byte{0x5a, 0x17, 0xc3}}
	m, err := Mint(context(1, Frontier{}, Known{}), NewDrafts(topCreate, topCreate), keyOf(1).Signer(), drawing(first, sharing(first, 4)))
	if err != nil {
		t.Fatal(err)
	}
	x, y := newItemOf(m.Events[0]), newItemOf(m.Events[1])
	if x.Width != 3 || y.Width != 5 {
		t.Errorf("two creates sharing 4 digits got widths %d and %d; want 3, then 5", x.Width, y.Width)
	}
}

func TestAnEntropySourceThatRepeatsFailsMint(t *testing.T) {
	var b KnownBuilder
	b.Saw(anItem)
	if m, err := Mint(context(1, Frontier{}, b.Known()), NewDrafts(topCreate), keyOf(1).Signer(), drawing(anItem)); err == nil || !strings.Contains(err.Error(), "repeats") {
		t.Errorf("drawing a known id mints %v, %v; want an error naming the entropy source", m.Events, err)
	}
	if _, err := Mint(context(1, Frontier{}, Known{}), NewDrafts(topCreate, topCreate), keyOf(1).Signer(), drawing(anItem, anItem)); err == nil {
		t.Error("one command drawing one id twice mints")
	}
}

func TestAnEntropySourceThatFailsFailsMint(t *testing.T) {
	for name, src := range map[string]io.Reader{
		"no id":   bytes.NewReader(nil),
		"no salt": bytes.NewReader(anItem.b[:]),
	} {
		if m, err := Mint(context(1, Frontier{}, Known{}), NewDrafts(topCreate), keyOf(1).Signer(), src); err == nil || !strings.Contains(err.Error(), "drawing") {
			t.Errorf("entropy with %s mints %v, %v; want an error naming the draw", name, m.Events, err)
		}
	}
}

func TestKnownIsAValueTheBuilderNoLongerChanges(t *testing.T) {
	var b KnownBuilder
	b.Saw(anItem)
	b.Created(Top{})
	k := b.Known()
	b.Saw(anElement)
	b.Created(Top{})
	if !k.Holds(anItem) || k.Holds(anElement) || k.Fill(Top{}) != 1 {
		t.Errorf("a later record changed a Known already taken: holds the element %v, fill %d", k.Holds(anElement), k.Fill(Top{}))
	}
}

func TestEverySubjectRoundTripsAndNoOtherShapeReads(t *testing.T) {
	for _, a := range []About{Root{}, Item{ID: anItem}, NewItem{ID: anItem, Width: 4}, Element{Of: anItem, ID: anElement},
		NewElement{Of: anItem, ID: anElement, Width: 26}} {
		if got, err := readAbout(aboutWire(a)); err != nil || got != a {
			t.Errorf("%#v reads back as %#v, %v", a, got, err)
		}
	}
	subject := func(fields func(*wire.Writer)) []byte {
		var w wire.Writer
		fields(&w)
		return w.Encoded()
	}
	for name, b := range map[string][]byte{
		"an element of the root": subject(func(w *wire.Writer) { w.Bytes(subjectElement, anElement.b[:]) }),
		"a segment of the root":  subject(func(w *wire.Writer) { w.Uvarint(subjectWidth, 3) }),
		"a short item id":        subject(func(w *wire.Writer) { w.Bytes(subjectItem, anItem.b[:15]) }),
		"a long element id":      subject(func(w *wire.Writer) { w.Bytes(subjectItem, anItem.b[:]); w.Bytes(subjectElement, make([]byte, 17)) }),
		"a two-digit segment":    subject(func(w *wire.Writer) { w.Bytes(subjectItem, anItem.b[:]); w.Uvarint(subjectWidth, 2) }),
		"a segment past the id":  subject(func(w *wire.Writer) { w.Bytes(subjectItem, anItem.b[:]); w.Uvarint(subjectWidth, 27) }),
	} {
		if got, err := readAbout(b); err == nil {
			t.Errorf("%s reads as %#v", name, got)
		}
	}
}

func TestAShortIDParsesBareOrUnderTheActivePrefixOnly(t *testing.T) {
	for s, want := range map[string]ShortID{
		"1f9":                    Segments{s: "1f9"},
		"1f9.zc7":                Segments{s: "1f9.zc7"},
		"lit-1f9.zc7":            Segments{s: "1f9.zc7"},
		anItem.String():          FullID{ID: anItem},
		"lit-" + anItem.String(): FullID{ID: anItem},
		// Crockford's decoding: case ignored, i and l are 1, o is 0
		"LIT-1F9.ZC7": Segments{s: "1f9.zc7"},
		"1fi":         Segments{s: "1f1"},
		"lo0":         Segments{s: "100"},
	} {
		if got, err := ParseShortID(s, prefix); err != nil || got != want {
			t.Errorf("ParseShortID(%q) = %#v, %v; want %#v", s, got, err, want)
		}
	}
	if _, err := ParseShortID("rin-1f9", prefix); err == nil || !strings.Contains(err.Error(), "rin") || !strings.Contains(err.Error(), "another project") {
		t.Errorf("an id under another prefix reads, or its refusal does not name it: %v", err)
	}
	for _, s := range []string{"", "1f", "1f9.", "1f9..zc7", "1fu", "lit-", "lit_1f9", "1f9/zc7", "unfiled"} {
		if got, err := ParseShortID(s, prefix); err == nil {
			t.Errorf("ParseShortID(%q) = %#v; want it refused", s, got)
		}
	}
	// a whole id's last digit holds two zero bits, as a segment or a full id
	if _, err := ParseShortID(anItem.String()[:fullWidth-1]+"z", prefix); err == nil || !strings.Contains(err.Error(), "two zeros") {
		t.Errorf("26 digits whose last holds a set pad bit: %v; want the pad bits named", err)
	}
	segs := Segments{s: "1f9.zc7"}.List()
	if got := Render(mustPrefix("abc"), anItem, segs); got != "abc-1f9.zc7" {
		t.Errorf("1f9.zc7 under abc renders %q", got)
	}
	if got := Render(prefix, anItem, nil); got != "lit-"+anItem.String() {
		t.Errorf("an item with no segments renders %q; want its full id", got)
	}
}

func TestKDraftsTakeTheKPositionsPastTheFrontierAndAnObserverSortsAfter(t *testing.T) {
	act := Act{About: Root{}, Payload: note{Text: text("x")}}
	m := mint(t, context(1, Frontier{highest: 99}, Known{}), keyOf(1), 1, act, act, act, act)
	var got []uint64
	for _, e := range m.Events {
		got = append(got, e.Header().Position().n)
	}
	if !slices.Equal(got, []uint64{100, 101, 102, 103}) {
		t.Errorf("four drafts past 99 took %v; want 100 to 103", got)
	}
	// a writer far behind, whose id sorts first, observes the last of them
	seen := m.Events[3]
	after := mint(t, context(0, Frontier{highest: 3}.Observe(seen.Header()), Known{}), keyOf(2), 2, act).Events[0]
	if Compare(seen.Order(), after.Order()) >= 0 {
		t.Errorf("an event minted after observing %s sorts before it", seen.ID())
	}
	if f := (Frontier{highest: 500}).Observe(seen.Header()); f.highest != 500 {
		t.Errorf("observing an older event moved the frontier back to %d", f.highest)
	}
	// two commands of one checkout racing one frontier, with no lock
	x := mint(t, context(1, Frontier{highest: 7}, Known{}), keyOf(1), 1, act).Events[0]
	y := mint(t, context(1, Frontier{highest: 7}, Known{}), keyOf(1), 2, act).Events[0]
	if x.Header().Position() != y.Header().Position() || Compare(x.Order(), y.Order()) == 0 || x.ID() == y.ID() {
		t.Errorf("two commands at one position are not told apart by the order")
	}
}

func TestAnEventIDReadsBackAndAShortOneIsRefused(t *testing.T) {
	id := mint(t, context(3, Frontier{highest: 1 << 40}, Known{}), keyOf(1), 1, Act{About: Root{}, Payload: note{Text: text("x")}}).Events[0].ID()
	if got, err := ReadEventID(id[:]); err != nil || got != id {
		t.Errorf("an event id reads back as %v, %v", got, err)
	}
	if _, err := ReadEventID(id[1:]); err == nil {
		t.Errorf("a short event id reads; want it refused")
	}
}
