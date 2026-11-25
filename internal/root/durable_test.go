package root

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/verbs"
)

// The rows of a run whose writes land and whose syncs then fail: written,
// said as written, and failed durability, never "nothing was written".

var errSync = errors.New("the fsync failed")

// unsynced is a session over a fresh twin whose store's logs answer
// faults, its workspace committed as admin init applies it.
func unsynced(t *testing.T, faults filestest.Faults) session {
	twin := freshTwinOver(filestest.NotDurable(files.NewMemory(), faults))
	twin.Code.Commit(map[string][]byte{verbs.WorkspacePath(): assets.DefaultWorkspace.Bytes()})
	return session{t: t, twin: &twin}
}

// notDurableRun is r failed durability naming want, failing t otherwise.
func notDurableRun(t *testing.T, r said, want string) {
	t.Helper()
	if r.status != 78 || reasonOf(r.event) != "durability" || !strings.Contains(r.err, want) || !strings.Contains(r.err, errSync.Error()) {
		t.Errorf("the run is status %d, reason %q, stderr %q; want 78, durability, naming %q and the sync's error", r.status, reasonOf(r.event), r.err, want)
	}
}

// landedNotDurable is whether e's one append landed its frame, not durable.
func landedNotDurable(e command.Event) bool {
	if len(e.Store.Appends) != 1 {
		return false
	}
	a, ok := e.Store.Appends[0].(store.AppendedNotDurable)
	return ok && errors.Is(a.Why, errSync)
}

func TestAFrameWhoseSyncFailedIsWrittenAndSaid(t *testing.T) {
	s := unsynced(t, filestest.Faults{Append: errSync})
	t.Run("admin init's founding frame keeps its pin and names it in the founding file", func(t *testing.T) {
		r := s.lit("admin", "init")
		notDurableRun(t, r, "a frame of")
		if strings.Contains(r.err, "remove") || !landedNotDurable(r.event) {
			t.Errorf("init's stderr is %q and its appends %+v; want no advice to remove the pin, and the frame landed, not durable", r.err, r.event.Store.Appends)
		}
		pinnedTo, err := pinned(s.twin.Process.Pin(git.Checkout{}))
		named, ferr := foundingFileIn(s.twin.Tree)
		if err != nil || ferr != nil || !reflect.DeepEqual(pinnedTo, named) {
			t.Fatalf("the pin names %v, %v and the founding file %v, %v; want both, one founding", pinnedTo, err, named, ferr)
		}
		f, founded := pinnedTo.(fold.FoundedBy)
		if !founded || !strings.Contains(r.out, "founding "+f.Enrollment.String()+" named in") {
			t.Errorf("init printed %q; want the founding it pinned, %v, said", r.out, pinnedTo)
		}
	})
	t.Run("a verb's frame is echoed, then fails durability, and is in the store once", func(t *testing.T) {
		r := s.lit("add", "unfiled/children", `{title: "landed"}`)
		notDurableRun(t, r, "running this command again may write it twice")
		if !landedNotDurable(r.event) {
			t.Errorf("add's appends are %+v; want the frame landed, not durable", r.event.Store.Appends)
		}
		id := filed(t, r.out)
		if got := s.ok("get", id+"/title"); got != "landed\n" {
			t.Errorf("the item's title reads %q; want landed", got)
		}
		if got := s.ok("get", "unfiled/children"); got != id+"\n" {
			t.Errorf("unfiled's children read\n%s\nwant the item once, %s", got, id)
		}
	})
}

// unreadKey is a shared identity port whose key file does not read back.
type unreadKey struct{ files.Files }

var errKeyUnread = errors.New("the key file does not read")

func (u unreadKey) ReadAll(n files.Name) ([]byte, error) {
	if n == command.NameOf(command.PrincipalFile{}) {
		return nil, errKeyUnread
	}
	return u.Files.ReadAll(n)
}

func TestTheRecoveryPhraseIsShownWheneverTheKeyWasCreated(t *testing.T) {
	// mint is a fresh session whose identity files are fresh, the shared
	// one as shared makes it over the files it is given, which it returns.
	mint := func(t *testing.T, shared func(files.Files) files.Files) (session, *files.Memory) {
		s := unsynced(t, filestest.Faults{})
		under := files.NewMemory()
		s.twin.Process.IdentityFiles = func(git.Checkout) (files.Files, files.Files) { return shared(under), files.NewMemory() }
		return s, under
	}
	// phrase is the phrase of the key under holds, which init must print.
	phrase := func(t *testing.T, under *files.Memory) string {
		t.Helper()
		b, err := under.ReadAll(command.NameOf(command.PrincipalFile{}))
		if err != nil {
			t.Fatal(err)
		}
		key, err := events.ParseKeyFile(b)
		if err != nil {
			t.Fatal(err)
		}
		return "principal " + key.Principal().String() + "\nrecovery phrase: " + key.Phrase().String() + "\n"
	}
	keyPath := command.PathOf(command.PrincipalFile{}, twinCheckout)
	t.Run("a key whose sync failed", func(t *testing.T) {
		s, under := mint(t, func(f files.Files) files.Files { return filestest.NotDurable(f, filestest.Faults{CreateOnce: errSync}) })
		r := s.lit("admin", "init")
		notDurableRun(t, r, keyPath)
		if !strings.HasPrefix(r.out, phrase(t, under)) {
			t.Errorf("init printed %q; want the key's principal and phrase first", r.out)
		}
		if got := r.event.Store.Creates; len(got) < 2 || got[1].Path != keyPath ||
			!reflect.DeepEqual(got[1].Placing, files.PlacedNotDurable{Made: files.Created, Swept: files.Swept{Reaped: []string{}, Left: []files.Leftover{}}, Why: errSync}) {
			t.Errorf("init's creates are %+v; want the key file created, not durable, second", got)
		}
	})
	t.Run("a key that does not read back", func(t *testing.T) {
		s, under := mint(t, func(f files.Files) files.Files { return unreadKey{f} })
		r := s.lit("admin", "init")
		if r.status != 78 || reasonOf(r.event) != "store" || !strings.Contains(r.err, errKeyUnread.Error()) {
			t.Errorf("init is status %d, reason %q, stderr %q; want the readback's failure", r.status, reasonOf(r.event), r.err)
		}
		if r.out != phrase(t, under)+"write it down: it is the one way to restore this key\n" {
			t.Errorf("init printed %q; want the key's principal and phrase", r.out)
		}
	})
}

// A create of the work tree's files whose sync failed is written: init goes
// on, and fails durability naming the file.
func TestAWorkTreeFileWhoseSyncFailedEndsInitNotDurable(t *testing.T) {
	s := unsynced(t, filestest.Faults{})
	tree := files.NewMemory()
	s.twin.Process.WorkTree = func(git.Checkout) files.Files {
		return filestest.NotDurable(tree, filestest.Faults{CreateOnce: errSync})
	}
	r := s.lit("admin", "init")
	notDurableRun(t, r, filepath.Join(twinCheckout.WorkTree, foundingPath()))
	if !strings.Contains(r.err, filepath.Join(twinCheckout.WorkTree, verbs.WorkspacePath())) || !strings.Contains(r.err, "running lit2 admin init again syncs it") {
		t.Errorf("init's stderr is %q; want the workspace file named too, and the rerun said safe", r.err)
	}
	if named, err := foundingFileIn(tree); err != nil || !strings.Contains(r.out, "applied as version one") {
		t.Errorf("the founding file names %v, %v, and init printed %q; want it written, and version one applied", named, err, r.out)
	}
}

// Every command in a checkout says the temps beside the checkout's own
// files that no create has reaped, a crash's, and the files whose temps do
// not read; never relying on git to show them.
func TestPlaceSaysTheCheckoutsLeftoverTemps(t *testing.T) {
	dir := checkout(t, minted)
	c := git.Checkout{WorkTree: dir, GitDir: filepath.Join(dir, ".git"), CommonDir: filepath.Join(dir, ".git")}
	plant := func(d files.Dir, n files.Name) string {
		t.Helper()
		if err := os.MkdirAll(d.Path(), 0o755); err != nil {
			t.Fatal(err)
		}
		temp := ".tmp-" + n.String() + "-0"
		if err := os.WriteFile(filepath.Join(d.Path(), temp), []byte("a crash's"), 0o600); err != nil {
			t.Fatal(err)
		}
		return temp
	}
	type file struct {
		at files.Dir
		n  files.Name
	}
	all := []file{
		{command.DirOf(command.IDFile{}, c), command.NameOf(command.IDFile{})},
		{command.DirOf(command.PrincipalFile{}, c), command.NameOf(command.PrincipalFile{})},
		{pinDir(c), pinFile},
		{litDir(c), foundingFile},
		{litDir(c), verbs.WorkspaceFile()},
	}
	var want []command.Leftovers
	for _, f := range all {
		want = append(want, command.TempsBeside{Path: f.at.PathOf(f.n), Temps: []string{plant(f.at, f.n)}})
	}
	in, ok := place(gittest.Runner(t), dir).(command.InCheckout)
	if !ok || !reflect.DeepEqual(in.Temps, want) {
		t.Errorf("the place's temps are %#v; want each file's own temp, by its path: %#v", in.Temps, want)
	}
	t.Run("a .lit that is no directory has its files' temps unread", func(t *testing.T) {
		if err := os.RemoveAll(litDir(c).Path()); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), litDir(c).Path()); err != nil {
			t.Fatal(err)
		}
		in := place(gittest.Runner(t), dir).(command.InCheckout)
		if len(in.Temps) != 5 {
			t.Fatalf("the place's temps are %#v; want the three identity files' and .lit's two unread", in.Temps)
		}
		for i, f := range all[3:] {
			if got, ok := in.Temps[3+i].(command.TempsUnread); !ok || got.Path != f.at.PathOf(f.n) || !errors.As(got.Why, new(files.NotTheDirectory)) {
				t.Errorf("%s's temps are %#v; want unread, the symlink refused", f.at.PathOf(f.n), in.Temps[3+i])
			}
		}
	})
}
