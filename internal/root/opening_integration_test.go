//go:build integration

package root

import (
	"crypto/rand"
	"os"
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/git/gittest"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

func TestAMintedCheckoutOpensItsStoreUnderTheCommonDir(t *testing.T) {
	for config, want := range map[string]git.ObjectFormat{
		"": git.SHA1, // no config at all: git's defaults
		"[core]\n\trepositoryformatversion = 0\n":                                        git.SHA1,
		"[core]\n\trepositoryformatversion = 1\n[extensions]\n\tobjectFormat = sha256\n": git.SHA256,
	} {
		dir := checkout(t, minted)
		if config != "" {
			if err := os.WriteFile(gittest.GitPath(t, dir, "config"), []byte(config), 0o600); err != nil {
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
	// outside a checkout is the one place whose reason says so; an
	// unminted repository there would open no store for another reason
	outside, g := gittest.NoCheckout(t)
	if got, ok := opening(place(g, outside, files.NewDurability()), diskStore(gittest.Runner(t), rand.Reader, files.NewDurability())).(command.Unopened); !ok || got.Why == nil || got.Why.Error() != "no git checkout at or above "+outside {
		t.Errorf("outside a checkout opens %#v; want no store, for no git checkout at or above %s", got, outside)
	}
	unminted := checkout(t, nil)
	for name, p := range map[string]command.Place{
		"an unminted one": place(gittest.Runner(t), unminted, files.NewDurability()),
		"a config that does not parse": place(gittest.Runner(t), func() string {
			d := checkout(t, minted)
			if err := os.WriteFile(gittest.GitPath(t, d, "config"), []byte("[core\n"), 0o600); err != nil {
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
