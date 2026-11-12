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

// three appends three item creates to the twin's store as its own writer.
func three(t *testing.T, twin Twin) {
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
		events.NewDrafts(create, create, create), twinKey().Signer(), rand.NewChaCha8([32]byte{3}))
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

func TestASecondOpenFoldsNothing(t *testing.T) {
	twin := NewTwin()
	three(t, twin)
	_, first := run(t, twin, "get", "lit-abc/title")
	f := folded(t, first)
	if f.Records != 3 || f.Checked != 3 || f.Counts.Items != 3 || f.Base != (fold.Refolded{Why: fold.Missed{Miss: store.Absent{}}}) {
		t.Errorf("the first open folds %+v; want the 3 records from genesis, absent", f)
	}
	if len(first.Store.Swaps) != 1 {
		t.Errorf("the first open swapped %d snapshots; want 1", len(first.Store.Swaps))
	}
	twin.Commands.Events = nil
	_, second := run(t, twin, "get", "lit-abc/title")
	if g := folded(t, second); g.Records != 0 || g.Checked != 0 || g.Counts.Items != 3 || g.Base != (fold.Extended{}) {
		t.Errorf("the second open folds %+v; want records=0, extended, the 3 items kept", g)
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
