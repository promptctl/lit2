package root

import (
	"slices"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
	"github.com/promptctl/lit2/internal/verbs"
)

// appendOne appends one record through s and reads every record of its
// writer's stream back.
func appendOne(t *testing.T, s *store.Store, me store.CheckoutID) []string {
	t.Helper()
	b, err := store.NewBatch(store.Record("an event"))
	if err != nil {
		t.Fatal(err)
	}
	storetest.AppendAll(t, s, b)
	h, err := s.Heads()
	if err != nil || len(h.Streams) != 1 || h.Streams[0].Ref != (store.WriterRef{Checkout: me}) {
		t.Fatalf("after one append the heads are %+v, %v; want the writer's one stream", h, err)
	}
	scan, err := s.Read(store.Range{Stream: h.Streams[0], From: 0})
	if err != nil {
		t.Fatal(err)
	}
	var rs []string
	for _, f := range scan.Frames {
		for _, r := range f.Batch.Records() {
			rs = append(rs, string(r))
		}
	}
	return rs
}

// outside is twin run where git finds no checkout.
func outside(twin Twin) Twin {
	twin.Process.Locate = func() (string, git.Checkout, error) {
		return "/nowhere", git.Checkout{}, git.NotFound{Dir: "/nowhere"}
	}
	return twin
}

// withIDFile is twin whose checkout's id file holds text, its key file
// the twin's own; with no text, there is no id file.
func withIDFile(t *testing.T, twin Twin, text []byte) Twin {
	t.Helper()
	shared, _ := twin.Process.IdentityFiles(twinCheckout)
	private := files.NewMemory(command.DirOf(command.IDFile{}, twinCheckout))
	if text != nil {
		durably(private.CreateOnce(command.NameOf(command.IDFile{}), text))
	}
	twin.Process.IdentityFiles = func(git.Checkout) (files.Files, files.Files) { return shared, private }
	return twin
}

// streams is the writers whose streams s holds.
func streams(t *testing.T, s *store.Store) []store.Ref {
	t.Helper()
	h, err := s.Heads()
	if err != nil {
		t.Fatal(err)
	}
	var refs []store.Ref
	for _, st := range h.Streams {
		refs = append(refs, st.Ref)
	}
	return refs
}

// writerOn is the writer e's place names, failing t unless it is minted.
func writerOn(t *testing.T, e command.Event) store.CheckoutID {
	t.Helper()
	in, ok := e.Place.(command.InCheckout)
	id, minted := in.Identity.(command.Minted)
	if !ok || !minted {
		t.Fatalf("the event's place is %#v; want a minted checkout", e.Place)
	}
	return id.Checkout
}

func TestTheTwinsStoreIsInMemory(t *testing.T) {
	if got := appendOne(t, NewFreshTwin().Store(), twinID); !slices.Equal(got, []string{"an event"}) {
		t.Errorf("the twin's store reads back %v", got)
	}
}

// The writer a run's event names is the one its id file holds, and the
// stream its write lands in is that writer's: the store is opened from
// the place, never beside it.
func TestARunWritesAsTheIDItsIdentityFilesHold(t *testing.T) {
	twin := withIDFile(t, NewTwin(), besideID.FileText())
	_, e := run(t, twin, "add", "unfiled/children", "{title: f}")
	if _, failed := e.Outcome.(cli.Failed); failed {
		t.Fatalf("add ended %#v; want it done", e.Outcome)
	}
	if got := writerOn(t, e); got != besideID {
		t.Errorf("the event names writer %s; want the id file's, %s", got, besideID)
	}
	if got := streams(t, twin.Store()); !slices.Contains(got, store.Ref(store.WriterRef{Checkout: besideID})) {
		t.Errorf("the store holds the streams %v; want one of %s, the writer the event names", got, besideID)
	}
}

// A twin whose id file does not parse is read as lit2 reads one on disk:
// no store opens, a verb fails as the store's, and the place says why.
func TestATwinWhoseIDFileDoesNotParseOpensNoStore(t *testing.T) {
	twin := withIDFile(t, NewTwin(), []byte("not an id"))
	_, e := run(t, twin, "get", "unfiled")
	in, _ := e.Place.(command.InCheckout)
	if _, unreadable := in.Identity.(command.Unreadable); !unreadable {
		t.Errorf("the event's place is %#v; want the checkout, its id file unreadable", e.Place)
	}
	failed, ok := e.Outcome.(cli.Failed)
	if !ok || failed.Failure.Reason() != "store" || len(e.Store.Folds) != 0 {
		t.Errorf("get ended %#v with folds %v; want it failed as the store's, no fold", e.Outcome, e.Store.Folds)
	}
	if want := command.PathOf(command.IDFile{}, twinCheckout); !strings.Contains(twin.Err.String(), want) {
		t.Errorf("stderr says %q; want it to name %s, the file that does not read", twin.Err, want)
	}
}

// admin init in a checkout with no identity mints its writer, and the
// writes after it are that writer's, in its stream.
func TestInitInAnUnmintedTwinMintsTheWriterItWritesAs(t *testing.T) {
	twin := NewFreshTwin()
	twin.Code.Commit(map[string][]byte{verbs.WorkspacePath(): assets.DefaultWorkspace.Bytes()})
	shared := files.NewMemory(command.DirOf(command.PrincipalFile{}, twinCheckout))
	private := files.NewMemory(command.DirOf(command.IDFile{}, twinCheckout))
	twin.Process.IdentityFiles = func(git.Checkout) (files.Files, files.Files) { return shared, private }
	if status := Run(twin.Process, []string{"admin", "init"}); status != 0 {
		t.Fatalf("admin init exited %d: %s", status, twin.Err)
	}
	twin.Commands.Events = nil
	_, e := run(t, twin, "add", "unfiled/children", "{title: f}")
	if _, failed := e.Outcome.(cli.Failed); failed {
		t.Fatalf("add ended %#v; want it done", e.Outcome)
	}
	minted := writerOn(t, e)
	if minted == twinID {
		t.Errorf("the run after init writes as %s, the twin's own id; want the id init minted", minted)
	}
	if got := streams(t, twin.Store()); !slices.Equal(got, []store.Ref{store.WriterRef{Checkout: minted}}) {
		t.Errorf("the store holds the streams %v; want the minted writer's alone, %s", got, minted)
	}
}

// Every invocation records the place it read, which says whether its store
// opened: a minted checkout's, and no other place's, a run that touched no
// store included.
func TestEveryInvocationRecordsThePlaceItsStoreOpenedAt(t *testing.T) {
	for name, tc := range map[string]struct {
		twin  Twin
		opens bool
	}{
		"a minted checkout":  {NewTwin(), true},
		"an unminted one":    {withIDFile(t, NewTwin(), nil), false},
		"outside a checkout": {outside(NewTwin()), false},
	} {
		_, e := run(t, tc.twin, "--help")
		if _, opens := command.OpeningAt(e.Place).(command.Opens); opens != tc.opens {
			t.Errorf("%s: the event's place is %#v, which opens a store: %v; want %v", name, e.Place, opens, tc.opens)
		}
	}
}

func TestTheTwinsStoreMovesNoTraceID(t *testing.T) {
	quiet, busy := NewTwin(), NewTwin()
	// creating the log draws its id from the store's entropy
	appendOne(t, busy.Store(), twinID)
	_, q := run(t, quiet, "--help")
	_, b := run(t, busy, "--help")
	if q.Trace != b.Trace {
		t.Errorf("a twin whose store created a log minted trace %s; one that did not, %s", b.Trace, q.Trace)
	}
}
