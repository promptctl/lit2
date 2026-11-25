package root

import (
	"crypto/rand"
	"errors"
	"github.com/promptctl/lit2/internal/git/gittest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
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

func TestAMintedCheckoutOpensItsStoreUnderTheCommonDir(t *testing.T) {
	for config, want := range map[string]git.ObjectFormat{
		"": git.SHA1, // no config at all: git's defaults
		"[core]\n\trepositoryformatversion = 0\n":                                        git.SHA1,
		"[core]\n\trepositoryformatversion = 1\n[extensions]\n\tobjectFormat = sha256\n": git.SHA256,
	} {
		dir := checkout(t, minted)
		if config != "" {
			if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(config), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		p := place(gittest.Runner(t), dir, files.NewDurability())
		in, located := p.(command.InCheckout)
		opened, ok := opening(p, diskStore(gittest.Runner(t), rand.Reader, files.NewDurability())).(command.Opened)
		if !located || !ok || in.Checkout.Format != want {
			t.Fatalf("a minted checkout is %#v and opens %#v; want its store, format %s", p, opening(p, diskStore(gittest.Runner(t), rand.Reader, files.NewDurability())), want)
		}
		if got := appendOne(t, opened.Store, twinID); !slices.Equal(got, []string{"an event"}) {
			t.Errorf("the opened store reads back %v", got)
		}
		// the store is the common dir's: one opened from the common dir
		// alone reads the append, from its log, since nothing is published
		alone := git.Checkout{CommonDir: in.Checkout.CommonDir, Format: in.Checkout.Format}
		h, err := store.Open(store.CheckoutID{0xff}, store.DiskPorts(gittest.Runner(t), alone, rand.Reader, files.NewDurability()), store.DefaultSegmentBound).Heads()
		if err != nil || h.LogsRead != 1 || len(h.Streams) != 1 || h.Streams[0].Ref != (store.WriterRef{Checkout: twinID}) {
			t.Errorf("the store of the common dir alone holds %+v, %v; want the append in twinID's log", h, err)
		}
	}
}

func TestAPlaceWithNoWriterOpensNoStore(t *testing.T) {
	unminted := checkout(t, nil)
	for name, p := range map[string]command.Place{
		"outside a checkout": place(gittest.Runner(t), t.TempDir(), files.NewDurability()),
		"an unminted one":    place(gittest.Runner(t), unminted, files.NewDurability()),
		"a config that does not parse": place(gittest.Runner(t), func() string {
			d := checkout(t, minted)
			if err := os.WriteFile(filepath.Join(d, ".git", "config"), []byte("[core\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return d
		}(), files.NewDurability()),
	} {
		if got, ok := opening(p, diskStore(gittest.Runner(t), rand.Reader, files.NewDurability())).(command.Unopened); !ok || got.Why == nil {
			t.Errorf("%s opens %#v; want no store, and why", name, opening(p, diskStore(gittest.Runner(t), rand.Reader, files.NewDurability())))
		}
	}
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
