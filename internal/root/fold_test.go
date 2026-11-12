package root

import (
	"errors"
	"math/rand/v2"
	"strings"
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
// its own writer.
func three(t *testing.T, twin Twin, seed byte) {
	t.Helper()
	prefix, err := events.ParsePrefix("lit")
	if err != nil {
		t.Fatal(err)
	}
	config, err := git.ParseObjectID(git.SHA1, "2e65efe2a145dda7ee51d1741299f848e5bf752e")
	if err != nil {
		t.Fatal(err)
	}
	level, err := events.ParseLevel(1)
	if err != nil {
		t.Fatal(err)
	}
	create := events.Create{In: events.Children{Parent: events.Top{Prefix: prefix}}, Payload: events.ItemCreate{Place: events.AtLevel{Level: level}}}
	m, err := events.Mint(events.Context{Writer: twinID, Agent: events.Unlabeled{}, ConfigRef: config, Policy: 1, Now: twin.Process.Clock.Now()},
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

func TestEveryOpenFoldsEveryRecord(t *testing.T) {
	twin := NewTwin()
	three(t, twin, 1)
	_, first := run(t, twin, "get", "lit-abc/title")
	if f := folded(t, first); f.Records != 3 || f.Checked != 3 || f.Counts.Items != 3 {
		t.Errorf("the first open folds %+v; want the 3 records, each checked", f)
	}
	twin.Commands.Events = nil
	_, second := run(t, twin, "get", "lit-abc/title")
	if g := folded(t, second); g.Records != 3 || g.Checked != 3 || g.Counts.Items != 3 {
		t.Errorf("the second open folds %+v; want the same 3 records read and checked again", g)
	}
	three(t, twin, 2)
	twin.Commands.Events = nil
	_, third := run(t, twin, "get", "lit-abc/title")
	if g := folded(t, third); g.Records != 6 || g.Checked != 6 || g.Counts.Items != 6 {
		t.Errorf("the open after a write folds %+v; want all 6 records", g)
	}
}

func TestATornSpanIsSkippedAndCounted(t *testing.T) {
	twin := NewTwin()
	repo := git.NewMemory()
	files := store.NewMemoryFiles()
	ports := store.Ports{Files: files, Objects: repo, Refs: repo, Entropy: rand.NewChaCha8([32]byte{5})}
	twin.Process.Store = command.Opened{Store: store.Open(twinID, ports, store.DefaultSegmentBound), Format: git.SHA1}
	three(t, twin, 1)
	// a write that died part way, then one that finished after it
	if _, err := files.Append(store.LogPath(twinID), []byte("half a frame")); err != nil {
		t.Fatal(err)
	}
	three(t, twin, 2)
	_, e := run(t, twin, "get", "lit-abc/title")
	if f := folded(t, e); f.Records != 6 || f.Torn != 1 || f.Counts.Items != 6 {
		t.Errorf("an open over a torn span folds %+v; want the 6 records and one torn span", f)
	}
	// the body ran: get is not built, which only its body says
	if failed, ok := e.Outcome.(cli.Failed); !ok || failed.Failure.Reason() == "store" || !strings.Contains(failed.Failure.Error(), "not built") {
		t.Errorf("a verb over a torn span ends %#v; want its body's outcome", e.Outcome)
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
