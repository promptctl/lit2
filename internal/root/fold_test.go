package root

import (
	"errors"
	"math/rand/v2"
	"testing"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/reason"
	"github.com/promptctl/lit2/internal/store"
)

// three appends three item creates, drawn from seed, to the twin's store as
// its own writer, at the frontier of what the store holds.
func three(t *testing.T, twin Twin, seed byte) {
	t.Helper()
	config, err := git.ParseObjectID(git.SHA1, "2e65efe2a145dda7ee51d1741299f848e5bf752e")
	if err != nil {
		t.Fatal(err)
	}
	level, err := events.ParseLevel(1)
	if err != nil {
		t.Fatal(err)
	}
	create := events.CreateItem{Payload: events.ItemCreate{Place: events.AtLevel{Level: level}}}
	shared, _ := twin.Process.IdentityFiles(git.Checkout{})
	founding, err := pinned(shared)
	if err != nil {
		t.Fatal(err)
	}
	held, err := replay(twin.Process.Fold, founding, twin.Process.Store.(command.Opened).Store, &command.StoreFacts{})
	if err != nil {
		t.Fatal(err)
	}
	m, err := events.Mint(events.Context{Writer: twinID, Agent: events.Unlabeled{}, ConfigRef: config, Policy: held.State.Cites(),
		Frontier: held.State.Frontier(), Known: held.State.Known(), Now: twin.Process.Clock.Now()},
		events.NewDrafts(create, create, create), twinKey().Signer(), rand.NewChaCha8([32]byte{seed}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := twin.Process.Store.(command.Opened).Store.Append(m.Batch); err != nil {
		t.Fatal(err)
	}
}

// folded is the one fold e records.
func folded(t *testing.T, e command.Event) fold.FoldFacts {
	t.Helper()
	if len(e.Store.Folds) != 1 {
		t.Fatalf("the invocation recorded %d folds; want 1", len(e.Store.Folds))
	}
	return e.Store.Folds[0]
}

// TestEveryOpenFoldsEveryRecord: each open reads and checks every record,
// the enrollment and version one's apply among them.
func TestEveryOpenFoldsEveryRecord(t *testing.T) {
	twin := NewTwin()
	three(t, twin, 1)
	_, first := run(t, twin, "get", "lit-abc/title")
	if f := folded(t, first); f.Records != 5 || f.Checked != 5 || f.Counts.Items != 3 || f.Counts.Versions != 1 {
		t.Errorf("the first open folds %+v; want the 5 records, each checked", f)
	}
	twin.Commands.Events = nil
	_, second := run(t, twin, "get", "lit-abc/title")
	if g := folded(t, second); g.Records != 5 || g.Checked != 5 || g.Counts.Items != 3 {
		t.Errorf("the second open folds %+v; want the same 5 records read and checked again", g)
	}
	three(t, twin, 2)
	twin.Commands.Events = nil
	_, third := run(t, twin, "get", "lit-abc/title")
	if g := folded(t, third); g.Records != 8 || g.Checked != 8 || g.Counts.Items != 6 {
		t.Errorf("the open after a write folds %+v; want all 8 records", g)
	}
}

func TestATornSpanIsSkippedAndCounted(t *testing.T) {
	twin := NewFreshTwin()
	repo := git.NewMemory()
	files := store.NewMemoryFiles()
	ports := store.Ports{Files: files, Objects: repo, Refs: repo, Entropy: rand.NewChaCha8([32]byte{5})}
	opened := command.Opened{Store: store.Open(twinID, ports, store.DefaultSegmentBound)}
	twin.Process.Store, twin.Process.Open = opened, func(git.Checkout, command.Minted) command.Opening { return opened }
	if status := Run(twin.Process, []string{"admin", "init"}); status != 0 {
		t.Fatalf("admin init exited %d: %s", status, twin.Err)
	}
	twin.Commands.Events = nil
	three(t, twin, 1)
	// a write that died part way, then one that finished after it
	if _, err := files.Append(store.LogPath(twinID), []byte("half a frame")); err != nil {
		t.Fatal(err)
	}
	three(t, twin, 2)
	_, e := run(t, twin, "get", "lit-abc/title")
	if f := folded(t, e); f.Records != 8 || f.Torn != 1 || f.Counts.Items != 6 {
		t.Errorf("an open over a torn span folds %+v; want the enrollment, version one, the 6 creates and one torn span", f)
	}
	// the body ran: only it records a verbs block
	if failed, ok := e.Outcome.(cli.Failed); ok && failed.Failure.Reason() == "store" || e.Verbs == nil {
		t.Errorf("a verb over a torn span ends %#v with verbs %v; want its body's outcome", e.Outcome, e.Verbs)
	}
}

func TestAVerbWithNoStoreFailsAsTheEnvironments(t *testing.T) {
	twin := NewTwin()
	twin.Process.Store = command.Unopened{Why: errors.New("this checkout has no id")}
	_, e := run(t, twin, "get", "lit-abc/title")
	failed, ok := e.Outcome.(cli.Failed)
	if !ok || failed.Failure.Reason() != "store" || failed.Failure.Class() != reason.ClassEnvironment || len(e.Store.Folds) != 0 {
		t.Errorf("a verb with no store ends %#v with folds %v; want it failed as the store's, no fold", e.Outcome, e.Store.Folds)
	}
}

// The twin's entropy is seeded, so a twin mints the same ids on every run
// and a scenario that names an id it minted reads the same id each time
// (specifications/code-layout.md, "The composition root"). Nothing in the binary mints yet; this is the reader the minting
// verbs will draw from.
func TestEveryTwinMintsTheSameIDs(t *testing.T) {
	config, err := git.ParseObjectID(git.SHA1, "2e65efe2a145dda7ee51d1741299f848e5bf752e")
	if err != nil {
		t.Fatal(err)
	}
	level, err := events.ParseLevel(2)
	if err != nil {
		t.Fatal(err)
	}
	create := events.CreateItem{Payload: events.ItemCreate{Place: events.AtLevel{Level: level}}}
	minted := func() []events.ItemID {
		twin := NewTwin()
		m, err := events.Mint(events.Context{Writer: twinID, Agent: events.Unlabeled{}, ConfigRef: config, Policy: 1, Now: twin.Process.Clock.Now()},
			events.NewDrafts(create, create), twinKey().Signer(), twin.Process.Entropy)
		if err != nil {
			t.Fatal(err)
		}
		var ids []events.ItemID
		for _, e := range m.Events {
			ids = append(ids, e.Header().About().(events.NewItem).ID)
		}
		return ids
	}
	first, again := minted(), minted()
	if first[0] != again[0] || first[1] != again[1] || first[0] == first[1] {
		t.Errorf("two twins minted %v and %v; want the same two distinct ids", first, again)
	}
}
