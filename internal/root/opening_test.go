package root

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// appendOne appends one record through s and reads every record of its
// writer's stream back.
func appendOne(t *testing.T, s *store.Store, me store.CheckoutID) []string {
	t.Helper()
	b, err := store.NewBatch(store.Record("an event"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Append(b); err != nil {
		t.Fatal(err)
	}
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
		"[core]\n\trepositoryformatversion = 0\n":                                        git.SHA1,
		"[core]\n\trepositoryformatversion = 1\n[extensions]\n\tobjectFormat = sha256\n": git.SHA256,
	} {
		dir, _ := checkout(t, string(twinID.FileText()))
		if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(config), 0o600); err != nil {
			t.Fatal(err)
		}
		opened, ok := opening(place(dir), rand.Reader).(Opened)
		if !ok || opened.Format != want {
			t.Fatalf("a minted checkout opens %#v; want its store, format %s", opening(place(dir), rand.Reader), want)
		}
		if got := appendOne(t, opened.Store, twinID); !slices.Equal(got, []string{"an event"}) {
			t.Errorf("the opened store reads back %v", got)
		}
		if _, err := os.Stat(filepath.Join(dir, ".git", "lit", "store", "log", twinID.String())); err != nil {
			t.Errorf("the append did not land in the store under the common dir: %v", err)
		}
	}
}

func TestAPlaceWithNoWriterOpensNoStore(t *testing.T) {
	unminted, _ := checkout(t, "")
	for name, p := range map[string]command.Place{
		"outside a checkout": place(t.TempDir()),
		"an unminted one":    place(unminted),
		"no object format":   place(func() string { d, _ := checkout(t, string(twinID.FileText())); return d }()),
	} {
		if got, ok := opening(p, rand.Reader).(Unopened); !ok || got.Why == nil {
			t.Errorf("%s opens %#v; want no store, and why", name, opening(p, rand.Reader))
		}
	}
}

func TestTheTwinsStoreIsInMemory(t *testing.T) {
	twin := NewTwin()
	opened, ok := twin.Process.Store.(Opened)
	if !ok {
		t.Fatalf("the twin's store is %#v; want one opened", twin.Process.Store)
	}
	if got := appendOne(t, opened.Store, twinID); !slices.Equal(got, []string{"an event"}) {
		t.Errorf("the twin's store reads back %v", got)
	}
}
