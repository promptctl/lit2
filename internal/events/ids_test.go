package events

import (
	"slices"
	"strings"
	"testing"
)

func TestAnItemIDRoundTripsAndNothingElseParses(t *testing.T) {
	for _, s := range []string{"lit-abc", "lit-0zz.792a.dee", "rin-2ae", "p2p-9xyzwv0", "lit-abc.def.ghj.kmn"} {
		id, err := ParseItemID(s)
		if err != nil || id.String() != s {
			t.Errorf("ParseItemID(%q) = %v, %v; want it back", s, id, err)
			continue
		}
		segs := id.Segments()
		if child := id.Parent().Child(segs[len(segs)-1]); child != id {
			t.Errorf("%s's parent's child of its last segment is %s", id, child)
		}
	}
	for _, s := range []string{"", "lit", "lit-", "li-abc", "Lit-abc", "1it-abc", "lit_abc", "lit-ab", "lit-abc.", "lit-abc..def",
		"lit-abi", "lit-abl", "lit-abo", "lit-abu", "lit-ABC", "lit-abc.de", "lit-abc/def", " lit-abc", "lit-abc-def"} {
		if id, err := ParseItemID(s); err == nil {
			t.Errorf("ParseItemID(%q) = %v; want it refused", s, id)
		}
	}
	if top, ok := mustID(t, "lit-abc").Parent().(Top); !ok || top.Prefix != prefix {
		t.Errorf("a top-level id's parent is %#v; want its prefix", mustID(t, "lit-abc").Parent())
	}
	if under, ok := mustID(t, "lit-abc.def").Parent().(Under); !ok || under.ID != mustID(t, "lit-abc") {
		t.Errorf("a child's parent is %#v; want lit-abc", mustID(t, "lit-abc.def").Parent())
	}
}

func mustID(t *testing.T, s string) ItemID {
	t.Helper()
	id, err := ParseItemID(s)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestWidthGrowsWithTheNamespaceAndKeepsAMatchUnlikely(t *testing.T) {
	prev := Width(0)
	for _, taken := range append(rangeTo(1<<16), 1<<20, 1<<24, 1<<30, 1<<40) {
		w := Width(taken)
		room := uint64(1) << (5 * w)
		if w < prev || w < 3 || 1024*(uint64(taken)+1) > room {
			t.Fatalf("Width(%d) = %d after %d; want it monotone, at least 3, and (taken+1)/32^w ≤ 1/1024", taken, w, prev)
		}
		if w > 3 && 1024*(uint64(taken)+1) <= room>>5 {
			t.Fatalf("Width(%d) = %d, though %d digits would do", taken, w, w-1)
		}
		prev = w
	}
	for taken, want := range map[int]int{0: 3, 31: 3, 32: 4, 1023: 4, 1024: 5, 32767: 5} {
		if got := Width(taken); got != want {
			t.Errorf("Width(%d) = %d; want %d", taken, got, want)
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

func TestTwoWritersAndTwoCommandsOfOneMintFromDistinctInputs(t *testing.T) {
	top := Children{Parent: Top{Prefix: prefix}}
	create := Create{In: top, Payload: made{Title: text("one title")}}
	at := Frontier{highest: 7}
	// two offline writers at one frontier, even drawing one salt stream
	a := mint(t, context(1, at, Names{}), keyOf(1), 1, create).Events[0]
	b := mint(t, context(2, at, Names{}), keyOf(2), 1, create).Events[0]
	// two commands of one checkout racing one frontier, with no lock
	c := mint(t, context(1, at, Names{}), keyOf(1), 2, create).Events[0]
	for _, pair := range [][2]Event{{a, b}, {a, c}, {b, c}} {
		x, y := pair[0], pair[1]
		if x.Header().Attribution().Checkout == y.Header().Attribution().Checkout && x.Header().PayloadHash() == y.Header().PayloadHash() {
			t.Errorf("two mints hashed one writer, position and payload hash")
		}
		if x.Header().About() == y.Header().About() {
			t.Errorf("two mints named one item, %v", x.Header().About())
		}
		if Compare(x.Order(), y.Order()) == 0 || Compare(x.Order(), y.Order()) != -Compare(y.Order(), x.Order()) {
			t.Errorf("the order does not tell %s from %s", x.ID(), y.ID())
		}
	}
	if a.Header().Position() != c.Header().Position() {
		t.Error("two commands at one frontier took different positions; the test does not race them")
	}
}

func TestMintNeverNamesWhatIsTakenAndLeavesNamesAsTheyWere(t *testing.T) {
	ns := Children{Parent: Top{Prefix: prefix}}
	create := Create{In: ns, Payload: made{Title: text("x")}}
	first := mint(t, context(1, Frontier{}, Names{}), keyOf(1), 5, create)
	name := first.Events[0].Header().About().(Item).ID.Segments()[0]

	var b NamesBuilder
	b.Add(ns, name)
	names := b.Names()
	again := mint(t, context(1, Frontier{}, names), keyOf(1), 5, create)
	if got := again.Events[0].Header().About().(Item).ID.Segments()[0]; got == name || again.Redraws != 1 {
		t.Errorf("with %s taken, the same draw named %s after %d redraws; want another name, one redraw", name, got, again.Redraws)
	}
	if names.Count(ns) != 1 || !names.Taken(ns, name) {
		t.Errorf("Mint changed its Names: %d taken", names.Count(ns))
	}
	b.Add(ns, Segment{s: "zzz"})
	if names.Count(ns) != 1 || names.Taken(ns, Segment{s: "zzz"}) || !b.Names().Taken(ns, Segment{s: "zzz"}) {
		t.Error("a later Add changed a Names already taken from the builder")
	}

	// two drafts of one command whose first draws name alike: the second
	// draws again
	s1, s2 := clashingSalts(t, ns)
	m, err := Mint(context(1, Frontier{}, Names{}), NewDrafts(create, create), keyOf(1).Signer(), saltStream(s1, s2, Salt{9}))
	if err != nil {
		t.Fatal(err)
	}
	x, y := m.Events[0].Header().About(), m.Events[1].Header().About()
	if x == y || m.Redraws != 1 {
		t.Errorf("one command named %v and %v after %d redraws; want two names, one redraw", x, y, m.Redraws)
	}
}

// clashingSalts finds two salts whose names at positions 1 and 2 match at
// three digits, by search: one in 32,768 pairs does.
func clashingSalts(t *testing.T, ns Namespace) (Salt, Salt) {
	t.Helper()
	payload := payloadWire(made{Title: text("x")})
	seen := map[Segment]Salt{}
	for i := range uint64(1 << 20) {
		var s Salt
		s[0], s[1], s[2] = byte(i), byte(i>>8), byte(i>>16)
		seen[segmentOf(nameDigest(writer(1), CausalPosition{n: 1}, payloadHash(s, payload)), 3)] = s
		var s2 Salt
		s2[31], s2[30], s2[29] = byte(i), byte(i>>8), byte(i>>16)
		if s1, ok := seen[segmentOf(nameDigest(writer(1), CausalPosition{n: 2}, payloadHash(s2, payload)), 3)]; ok {
			return s1, s2
		}
	}
	t.Fatalf("no clash in 2^20 salts in %v", ns)
	return Salt{}, Salt{}
}

// repeating is a broken salt source: every byte it gives is 7.
type repeating struct{}

func (repeating) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 7
	}
	return len(p), nil
}

func TestABrokenSaltSourceFailsMintInsteadOfHangingIt(t *testing.T) {
	ns := Children{Parent: Top{Prefix: prefix}}
	create := Create{In: ns, Payload: made{Title: text("x")}}
	c := context(1, Frontier{}, Names{})
	first, err := Mint(c, NewDrafts(create), keyOf(1).Signer(), repeating{})
	if err != nil {
		t.Fatal(err)
	}
	var b NamesBuilder
	b.Add(ns, first.Events[0].Header().About().(Item).ID.Segments()[0])
	c.Names = b.Names()
	if m, err := Mint(c, NewDrafts(create), keyOf(1).Signer(), repeating{}); err == nil || !strings.Contains(err.Error(), "salt") {
		t.Errorf("a salt source that repeats a taken name mints %v, %v; want an error naming the salt source", m.Events, err)
	}
}

type stream struct{ salts []Salt }

func saltStream(ss ...Salt) *stream { return &stream{salts: ss} }

func (s *stream) Read(p []byte) (int, error) {
	n := copy(p, s.salts[0][:])
	s.salts = s.salts[1:]
	return n, nil
}

func TestKDraftsTakeTheKPositionsPastTheFrontierAndAnObserverSortsAfter(t *testing.T) {
	act := Act{About: Root{}, Payload: note{Text: text("x")}}
	m := mint(t, context(1, Frontier{highest: 99}, Names{}), keyOf(1), 1, act, act, act, act)
	var got []uint64
	for _, e := range m.Events {
		got = append(got, e.Header().Position().n)
	}
	if !slices.Equal(got, []uint64{100, 101, 102, 103}) {
		t.Errorf("four drafts past 99 took %v; want 100 to 103", got)
	}
	// a writer far behind, whose id sorts first, observes the last of them
	seen := m.Events[3]
	after := mint(t, context(0, Frontier{highest: 3}.Observe(seen.Header()), Names{}), keyOf(2), 2, act).Events[0]
	if Compare(seen.Order(), after.Order()) >= 0 {
		t.Errorf("an event minted after observing %s sorts before it", seen.ID())
	}
	if f := (Frontier{highest: 500}).Observe(seen.Header()); f.highest != 500 {
		t.Errorf("observing an older event moved the frontier back to %d", f.highest)
	}
}

func TestTheStoredFormsOfPositionsOrderKeysAndIDsReadBack(t *testing.T) {
	e := mint(t, context(3, Frontier{highest: 1 << 40}, Names{}), keyOf(1), 1, Act{About: Root{}, Payload: note{Text: text("x")}}).Events[0]
	if p, err := ReadPosition(e.Header().Position().Wire()); err != nil || p != e.Header().Position() {
		t.Errorf("a position reads back as %v, %v", p, err)
	}
	if k, err := ReadOrderKey(e.Order().Wire()); err != nil || k != e.Order() {
		t.Errorf("an order key reads back as %v, %v", k, err)
	}
	if id, err := ReadEventID(e.ID().Wire()); err != nil || id != e.ID() {
		t.Errorf("an event id reads back as %v, %v", id, err)
	}
	for name, err := range map[string]error{
		"position 0":       errOf(ReadPosition(CausalPosition{}.Wire())),
		"a trailing byte":  errOf(ReadPosition(append(e.Header().Position().Wire(), 0))),
		"an empty key":     errOf(ReadOrderKey(nil)),
		"a key at 0":       errOf(ReadOrderKey(OrderKey{Writer: writer(1)}.Wire())),
		"a short event id": errOf(ReadEventID(e.ID().Wire()[1:])),
	} {
		if err == nil {
			t.Errorf("%s reads; want it refused", name)
		}
	}
}

func errOf[T any](_ T, err error) error { return err }
