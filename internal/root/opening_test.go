package root

import (
	"errors"
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
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

func TestTheTwinsStoreIsInMemory(t *testing.T) {
	twin := NewFreshTwin()
	opened, ok := twin.Process.Store.(command.Opened)
	if !ok {
		t.Fatalf("the twin's store is %#v; want one opened", twin.Process.Store)
	}
	if got := appendOne(t, opened.Store, twinID); !slices.Equal(got, []string{"an event"}) {
		t.Errorf("the twin's store reads back %v", got)
	}
}

func TestEveryInvocationRecordsItsStoresOpening(t *testing.T) {
	_, e := run(t, NewTwin(), "--help")
	if _, ok := e.Store.Opening.(command.Opened); !ok {
		t.Errorf("the twin's invocation records the opening %#v; want its store opened", e.Store.Opening)
	}
	unopened := NewTwin()
	why := errors.New("this checkout has no id")
	unopened.Process.Store = command.Unopened{Why: why}
	if _, e := run(t, unopened, "--help"); e.Store.Opening != (command.Unopened{Why: why}) {
		t.Errorf("an invocation with no store records %#v; want it unopened, and why", e.Store.Opening)
	}
}

func TestTheTwinsStoreMovesNoTraceID(t *testing.T) {
	quiet, busy := NewTwin(), NewTwin()
	// creating the log draws its id from the store's entropy
	appendOne(t, busy.Process.Store.(command.Opened).Store, twinID)
	_, q := run(t, quiet, "--help")
	_, b := run(t, busy, "--help")
	if q.Trace != b.Trace {
		t.Errorf("a twin whose store created a log minted trace %s; one that did not, %s", b.Trace, q.Trace)
	}
}
