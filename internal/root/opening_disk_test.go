//go:build integration

package root

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
	"github.com/promptctl/lit2/internal/store"
)

// readAt is the place lit2 reads at dir through g, and the store it opens
// there, as Run derives them over Main's ports.
func readAt(g *git.Git, dir string) (command.Place, situation) {
	u := files.NewDurability()
	p := Process{
		Locate: func() (string, git.Checkout, error) {
			c, err := g.Locate(dir)
			return dir, c, err
		},
		IdentityFiles: identityFiles(u),
		Stores:        diskStores(g, rand.Reader, u),
	}
	place := placeOf(p)
	return place, situate(p, place)
}

// placeAt is the place lit2 reads at dir through g.
func placeAt(g *git.Git, dir string) command.Place {
	place, _ := readAt(g, dir)
	return place
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
		p, o := readAt(gittest.Runner(t), dir)
		in, located := p.(command.InCheckout)
		opened, ok := o.(held)
		if !located || !ok || in.Checkout.Format != want {
			t.Fatalf("a minted checkout is %#v and opens %#v; want its store, format %s", p, o, want)
		}
		if got := appendOne(t, opened.store, twinID); !slices.Equal(got, []string{"an event"}) {
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
	outside, ceilingEnv := gittest.Outside(t)
	for name, dir := range map[string]struct {
		g   *git.Git
		dir string
	}{
		"outside a checkout": {gittest.Runner(t, ceilingEnv), outside},
		"an unminted one":    {gittest.Runner(t), unminted},
		"a config that does not parse": {gittest.Runner(t), func() string {
			d := checkout(t, minted)
			if err := os.WriteFile(filepath.Join(d, ".git", "config"), []byte("[core\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			return d
		}()},
	} {
		if _, o := readAt(dir.g, dir.dir); !isNoStore(o) {
			t.Errorf("%s opens %#v; want no store, and why", name, o)
		}
	}
}

// isNoStore is whether s is no store, said with why.
func isNoStore(s situation) bool {
	none, ok := s.(noStore)
	return ok && none.why != nil
}
