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

// diskAt is Main's ports of place and store as lit2 run at dir through g
// has them.
func diskAt(g *git.Git, dir string) Process {
	u := files.NewDurability()
	return Process{
		Locate:        func() command.Located { return locate(g, dir) },
		IdentityFiles: identityFiles(u),
		Stores:        diskStores(g, rand.Reader, u),
	}
}

// placeAt is the place lit2 reads at dir through g.
func placeAt(g *git.Git, dir string) command.Place { return placeOf(diskAt(g, dir)) }

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
		disk := diskAt(gittest.Runner(t), dir)
		p := placeOf(disk)
		in, located := p.(command.InCheckout)
		o, opens := command.OpeningAt(p).(command.Opens)
		if !located || !opens || in.Checkout.Format != want {
			t.Fatalf("a minted checkout is %#v and opens %#v; want its store, format %s", p, command.OpeningAt(p), want)
		}
		if got := appendOne(t, disk.writerOf(o).store, twinID); !slices.Equal(got, []string{"an event"}) {
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
		if o, none := command.OpeningAt(placeAt(dir.g, dir.dir)).(command.NoStore); !none || o.Why == nil {
			t.Errorf("%s opens %#v; want no store, and why", name, command.OpeningAt(placeAt(dir.g, dir.dir)))
		}
	}
}
