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

func TestASecondOpenFoldsNothing(t *testing.T) {
	twin := NewTwin()
	three(t, twin, 1)
	_, first := run(t, twin, "get", "lit-abc/title")
	f := folded(t, first)
	if f.Records != 3 || f.Checked != 3 || f.Counts.Items != 3 || f.Base != (fold.Refolded{Why: fold.Missed{Miss: store.Absent{}}}) {
		t.Errorf("the first open folds %+v; want the 3 records from genesis, absent", f)
	}
	if !swapped(first) {
		t.Errorf("the first open saves %v; want a snapshot swapped", first.Store.Swaps)
	}
	twin.Commands.Events = nil
	_, second := run(t, twin, "get", "lit-abc/title")
	if g := folded(t, second); g.Records != 0 || g.Checked != 0 || g.Counts.Items != 3 || g.Base != (fold.Extended{}) {
		t.Errorf("the second open folds %+v; want records=0, extended, the 3 items kept", g)
	}
	if len(second.Store.Swaps) != 1 || second.Store.Swaps[0] != command.Swap(command.Kept{}) {
		t.Errorf("the second open saves %v; want the snapshot kept, unwritten", second.Store.Swaps)
	}
	// a write moves the head past the snapshot's mark, so the next open
	// reads it and swaps again
	three(t, twin, 2)
	twin.Commands.Events = nil
	_, third := run(t, twin, "get", "lit-abc/title")
	if g := folded(t, third); g.Records != 3 || g.Counts.Items != 6 || !swapped(third) {
		t.Errorf("the open after a write folds %+v and saves %v; want its 3 records and a swap", g, third.Store.Swaps)
	}
}

// swapped is whether e's one save wrote a snapshot.
func swapped(e command.Event) bool {
	if len(e.Store.Swaps) != 1 {
		return false
	}
	_, ok := e.Store.Swaps[0].(command.Swapped)
	return ok
}

// readOnly is a store root whose snapshot cannot be written, as a full
// or read-only cache directory refuses it.
type readOnly struct{ *store.MemoryFiles }

var errReadOnly = errors.New("the cache directory is read-only")

func (r readOnly) Replace(p store.Path, b []byte) error {
	if p == store.SnapshotPath {
		return errReadOnly
	}
	return r.MemoryFiles.Replace(p, b)
}

func TestASnapshotThatCannotBeWrittenFailsNothing(t *testing.T) {
	twin := NewTwin()
	repo := git.NewMemory()
	ports := store.Ports{Files: readOnly{store.NewMemoryFiles()}, Objects: repo, Refs: repo, Entropy: rand.NewChaCha8([32]byte{4})}
	twin.Process.Store = command.Opened{Store: store.Open(twinID, ports, store.DefaultSegmentBound), Format: git.SHA1}
	three(t, twin, 1)
	_, e := run(t, twin, "get", "lit-abc/title")
	if len(e.Store.Swaps) != 1 || e.Store.Swaps[0] != command.Swap(command.SwapFailed{Why: errReadOnly}) {
		t.Errorf("a refused swap is recorded as %v; want it failed, and why", e.Store.Swaps)
	}
	// the body ran: get is not built, which only its body says
	if failed, ok := e.Outcome.(cli.Failed); !ok || failed.Failure.Reason() == "store" || !strings.Contains(failed.Failure.Error(), "not built") {
		t.Errorf("a verb whose swap failed ends %#v; want its body's outcome", e.Outcome)
	}
	if strings.Contains(twin.Err.String(), errReadOnly.Error()) {
		t.Errorf("the refused swap reached stderr: %q", twin.Err.String())
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

func TestAnUnreadablePayloadRecordsThePlanItFollowed(t *testing.T) {
	twin := NewTwin()
	three(t, twin, 1)
	s := twin.Process.Store.(command.Opened).Store
	heads, err := s.Heads()
	if err != nil {
		t.Fatal(err)
	}
	var marks []store.Mark
	for _, st := range heads.Streams {
		marks = append(marks, store.Mark{Ref: st.Ref, Upto: st.Until})
	}
	// a snapshot under this fold's tag, at the heads, whose payload no fold wrote
	if _, err := s.Swap(fold.Tag(), marks, []byte("not a fold's payload")); err != nil {
		t.Fatal(err)
	}
	_, e := run(t, twin, "get", "lit-abc/title")
	f := folded(t, e)
	if _, ok := f.Base.(fold.Refolded).Why.(fold.PayloadUnreadable); !ok || f.Records != 3 {
		t.Fatalf("an unreadable payload folds %+v; want a genesis of the 3 records, payload unreadable", f)
	}
	if len(e.Store.Plans) != 1 || len(e.Store.Plans[0].Delta) != 1 || e.Store.Plans[0].Delta[0].From != 0 {
		t.Errorf("the event records the plan %+v; want the one followed, every stream from 0", e.Store.Plans)
	}
	if _, ok := e.Store.Plans[0].Base.(store.Genesis); !ok {
		t.Errorf("the recorded plan resumes from %#v; want the genesis the fold ran", e.Store.Plans[0].Base)
	}
}
