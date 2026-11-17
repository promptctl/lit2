package tick

import (
	"bytes"
	"slices"
	"testing"
	"time"

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
	running  = must(config.ReadReaderVersion("v0.4.0"))
	schema   = must(config.Load(config.NeverCommitted{}, running)).Schema()
)

// mint folds d onto s as a writer that has folded s but knows only known,
// its create's id drawn as id.
func mint(t *testing.T, s fold.State, known events.Known, d events.Draft, id [16]byte) (fold.State, events.ItemID) {
	t.Helper()
	entropy := bytes.NewReader(append(id[:], make([]byte, 64)...))
	m, err := events.Mint(events.Context{Writer: store.CheckoutID{1}, Agent: events.Unlabeled{},
		ConfigRef: must(git.ParseObjectID(git.SHA1, "2e65efe2a145dda7ee51d1741299f848e5bf752e")), Policy: 1,
		Frontier: s.Frontier(), Known: known, Now: time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC)},
		events.NewDrafts(d), key.Signer(), entropy)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range fold.Check(env, m.Events) {
		s = fold.Step(registry, s, c)
	}
	switch a := m.Events[0].Header().About().(type) {
	case events.NewItem:
		return s, a.ID
	case events.Item:
		return s, a.ID
	}
	return s, events.ItemID{}
}

func create(p events.Placement) events.Draft {
	return events.CreateItem{Payload: events.ItemCreate{Place: p}}
}

// TestAncestryIsTheItemThenWhatItSitsIn: a story under a feature filed in
// unfiled reads through itself, the feature, and the home.
func TestAncestryIsTheItemThenWhatItSitsIn(t *testing.T) {
	var s fold.State
	s, feature := mint(t, s, s.Known(), create(events.AtLevel{Level: must(events.ParseLevel(2))}), [16]byte{0x10})
	s, story := mint(t, s, s.Known(), create(events.BelowParent{Parent: feature}), [16]byte{0x20})
	got := ItemAncestry(s, schema, story)
	want := config.Ancestry{config.ItemNode(story), config.ItemNode(feature), config.HomeNode("unfiled")}
	if !slices.Equal(got, want) {
		t.Errorf("the story's ancestry = %v; want %v", got, want)
	}
	if got := AncestryOf(s, schema, nil); len(got) != 0 {
		t.Errorf("no container reads through %v; want nothing", got)
	}
}

// TestAForgedCycleEnds: a parent link counts only when the parent's create
// sorts first, as the fold reads it, so a cycle two creates forge is no
// cycle and every ancestry ends.
func TestAForgedCycleEnds(t *testing.T) {
	a, b := [16]byte{0x10}, [16]byte{0x20}
	_, later := mint(t, fold.State{}, events.Known{}, create(events.AtLevel{Level: must(events.ParseLevel(2))}), b) // b's id, learned
	var s fold.State
	s, idA := mint(t, s, s.Known(), create(events.BelowParent{Parent: later}), a)
	s, idB := mint(t, s, events.Known{}, create(events.BelowParent{Parent: idA}), b)
	if idB != later {
		t.Fatalf("b minted %s, then %s", later, idB)
	}
	for _, id := range []events.ItemID{idA, idB} {
		if got := ItemAncestry(s, schema, id); len(got) > 2 {
			t.Errorf("%s reads through %v", id, got)
		}
	}
}

// TestStatusReadsThroughTheGraph: no transition reads the initial column,
// a new item's zero facts included.
func TestStatusReadsThroughTheGraph(t *testing.T) {
	eff := must(schema.Effective(nil))
	if got := StatusIn(eff, fold.Facts{}); got != "backlog" {
		t.Errorf("an unwritten status reads %s; want backlog", got)
	}
}
