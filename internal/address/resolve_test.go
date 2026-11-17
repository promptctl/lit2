package address_test

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/address"
	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

var (
	registry = fold.NewRegistry(fold.FirstFacts()...)
	env      = must(fold.NewEnv(registry, events.NewSuites(events.Ed25519()), 1))
	key      = must(events.NewKey(bytes.NewReader(bytes.Repeat([]byte{1}, 32))))
	cfgRef   = must(git.ParseObjectID(git.SHA1, "2e65efe2a145dda7ee51d1741299f848e5bf752e"))
	running  = must(config.ReadReaderVersion("v0.4.0"))
)

// entropy gives each id in order, then a zero salt, per create.
func entropy(ids ...[16]byte) *bytes.Reader {
	var b []byte
	for _, id := range ids {
		b = append(b, id[:]...)
		b = append(b, make([]byte, 32)...)
	}
	return bytes.NewReader(b)
}

// mint folds into s what writer w mints from st's view, its ids drawn from
// ids; and returns the state and the events.
func mint(t *testing.T, s, view fold.State, w byte, d events.Draft, ids ...[16]byte) (fold.State, events.Event) {
	t.Helper()
	m, err := events.Mint(events.Context{Writer: store.CheckoutID{w}, Agent: events.Unlabeled{}, ConfigRef: cfgRef, Policy: 1,
		Frontier: view.Frontier(), Known: view.Known(), Now: time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC)},
		events.NewDrafts(d), key.Signer(), entropy(ids...))
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range fold.Check(env, m.Events) {
		s = fold.Step(registry, s, c)
	}
	return s, m.Events[0]
}

func top(n int) events.Draft {
	return events.CreateItem{Payload: events.ItemCreate{Place: events.AtLevel{Level: must(events.ParseLevel(n))}}}
}

func under(p events.ItemID) events.Draft {
	return events.CreateItem{Payload: events.ItemCreate{Place: events.BelowParent{Parent: p}}}
}

func made(e events.Event) events.NewItem { return e.Header().About().(events.NewItem) }

func short(t *testing.T, s string) events.ShortID {
	t.Helper()
	return must(events.ParseShortID(s, events.KnownPrefixes(must(events.ParsePrefix("lit")))))
}

func TestASegmentTwoOfflineWritersFixedIsAmbiguousAndListsBoth(t *testing.T) {
	// A and B mint offline, neither having folded the other: ids sharing
	// their first three digits, so both fix the segment 000
	x, y := [16]byte{0, 0, 0x01}, [16]byte{0, 0, 0x02}
	var s fold.State
	s, ex := mint(t, s, fold.State{}, 0xa, top(2), x)
	s, ey := mint(t, s, fold.State{}, 0xb, top(2), y)
	a, b := made(ex), made(ey)
	if a.Segment() != b.Segment() || a.Segment().String() != "000" {
		t.Fatalf("the writers fixed %s and %s; the test wants one segment", a.Segment(), b.Segment())
	}
	_, err := address.Resolve(s, short(t, "lit-000"))
	var amb address.Ambiguous
	if !errors.As(err, &amb) || !slices.Contains(amb.Candidates, a.ID.String()) || !slices.Contains(amb.Candidates, b.ID.String()) || len(amb.Candidates) != 2 {
		t.Fatalf("000 resolves with %v; want it ambiguous, listing both items", err)
	}
	if !strings.Contains(err.Error(), a.ID.String()) || !strings.Contains(err.Error(), b.ID.String()) {
		t.Errorf("the refusal %q does not name both items", err)
	}
	for _, id := range []events.ItemID{a.ID, b.ID} {
		if got, err := address.Resolve(s, short(t, id.String())); err != nil || got != id {
			t.Errorf("the full id %s resolves to %s, %v; want itself", id, got, err)
		}
	}
	// a writer that folded both widens past them: its segment names it alone
	s, ez := mint(t, s, s, 0xa, top(2), [16]byte{0, 0, 0x03})
	if got, err := address.Resolve(s, short(t, made(ez).Segment().String())); err != nil || got != made(ez).ID {
		t.Errorf("a segment minted after both resolves to %s, %v; want its item", got, err)
	}
}

func TestAFullIDNamesOnlyACreatedItem(t *testing.T) {
	ghost := events.FullID{ID: must(events.ParseItemID("g0000000000000000000000000"))}
	s, _ := mint(t, fold.State{}, fold.State{}, 1, events.Act{About: events.Item(ghost),
		Payload: events.FieldSet{Field: must(events.ParseName("title")), Value: events.Prose{Body: events.SealPlain("t")}}}, [16]byte{})
	if _, ok := s.Item(ghost.ID).(fold.Found); !ok {
		t.Fatalf("the fold holds no record of %s, which a fact names", ghost)
	}
	if _, err := address.Resolve(s, ghost); !errors.As(err, new(address.NotFound)) {
		t.Errorf("an id facts only name resolves: %v; want NotFound", err)
	}
}

func TestSegmentsResolveByTheItemsOwnAndItsAncestors(t *testing.T) {
	var s fold.State
	s, epic := mint(t, s, s, 0xa, top(3), [16]byte{0x10})
	s, child := mint(t, s, s, 0xa, under(made(epic).ID), [16]byte{0x20})
	own := made(child).Segment().String()
	full := made(epic).Segment().String() + "." + own
	for _, typed := range []string{own, full, "lit-" + full} {
		if got, err := address.Resolve(s, short(t, typed)); err != nil || got != made(child).ID {
			t.Errorf("%s resolves to %s, %v; want the child", typed, got, err)
		}
	}
	var nf address.NotFound
	for _, typed := range []string{own + "." + own, "zzz", "zzz." + own} {
		if _, err := address.Resolve(s, short(t, typed)); !errors.As(err, &nf) {
			t.Errorf("%s resolves with %v; want not found", typed, err)
		}
	}
}

func TestEveryIDRendersUnderTheActiveVersionsPrefix(t *testing.T) {
	var s fold.State
	s, epic := mint(t, s, s, 0xa, top(3), [16]byte{0x10})
	s, child := mint(t, s, s, 0xa, under(made(epic).ID), [16]byte{0x20})
	before := s.Items()
	render := func(prefix string) map[events.ItemID]string {
		text := bytes.Replace(assets.DefaultWorkspace.Bytes(), []byte(`prefix = "lit"`), []byte(`prefix = "`+prefix+`"`), 1)
		schema, err := config.Parse(text, "workspace.toml", running)
		if err != nil {
			t.Fatal(err)
		}
		out := map[events.ItemID]string{}
		for _, it := range s.Items() {
			out[it.ID] = events.Render(schema.Prefix(), it.ID, s.Path(it.ID))
		}
		return out
	}
	lit, rin := render("lit"), render("rin")
	want := map[events.ItemID]string{
		made(epic).ID:  made(epic).Segment().String(),
		made(child).ID: made(epic).Segment().String() + "." + made(child).Segment().String(),
	}
	for id, segs := range want {
		if lit[id] != "lit-"+segs || rin[id] != "rin-"+segs {
			t.Errorf("%s renders %q, then %q; want lit-%s, then rin-%s", id, lit[id], rin[id], segs, segs)
		}
	}
	if !slices.EqualFunc(before, s.Items(), func(a, b fold.Item) bool { return a.ID == b.ID }) {
		t.Error("rendering under another prefix changed the items")
	}
}
