package root

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
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
		notDurableRun(t, r, "running this command again writes it twice")
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
			!reflect.DeepEqual(got[1].Placing, files.PlacedNotDurable{Placed: files.Placed{Made: files.Created}, Why: errSync}) {
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
	if !strings.Contains(r.err, filepath.Join(twinCheckout.WorkTree, verbs.WorkspacePath())) || !strings.Contains(r.err, "cannot be confirmed") || strings.Contains(r.err, "again syncs") {
		t.Errorf("init's stderr is %q; want the workspace file named too, said unconfirmed, and no rerun claimed to sync it", r.err)
	}
	if named, err := foundingFileIn(tree); err != nil || !strings.Contains(r.out, "applied as version one") {
		t.Errorf("the founding file names %v, %v, and init printed %q; want it written, and version one applied", named, err, r.out)
	}
}

// Every command in a checkout says the temps beside the checkout's own
// files that no create has reaped, a crash's, and the files whose temps do
// not read; never relying on git to show them.
func TestLeftoversSaysTheCheckoutsTemps(t *testing.T) {
	dir := checkout(t, minted)
	c := git.Checkout{WorkTree: dir, GitDir: filepath.Join(dir, ".git"), CommonDir: filepath.Join(dir, ".git")}
	disk := Process{IdentityFiles: identityFiles, Pin: pinFiles, WorkTree: workTree}
	var want []command.Leftovers
	for _, f := range disk.onceFiles(c).all() {
		want = append(want, command.TempsBeside{Dir: f.dir.Path(), Temps: []string{plant(t, f.dir.Path(), f.name.String())}})
	}
	if got := leftovers(disk, c, command.Unopened{Why: errors.New("unminted")}); !reflect.DeepEqual(got, want) {
		t.Errorf("the checkout's temps are %#v; want each file's own temp, by its directory: %#v", got, want)
	}
	t.Run("a .lit that is no directory has its files' temps unread", func(t *testing.T) {
		if err := os.RemoveAll(litDir(c).Path()); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), litDir(c).Path()); err != nil {
			t.Fatal(err)
		}
		got := leftovers(disk, c, command.Unopened{Why: errors.New("unminted")})
		if len(got) != 5 {
			t.Fatalf("the checkout's temps are %#v; want the three identity files' and .lit's two unread", got)
		}
		for _, l := range got[3:] {
			if u, ok := l.(command.TempsUnread); !ok || u.Dir != litDir(c).Path() || !errors.As(u.Why, new(files.NotTheDirectory)) {
				t.Errorf("the .lit temps are %#v; want unread, the symlink refused", l)
			}
		}
	})
}

// plant is a crash's temp of the file name in the directory at dir.
func plant(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	temp := ".tmp-" + name + "-0"
	if err := os.WriteFile(filepath.Join(dir, temp), []byte("a crash's"), 0o600); err != nil {
		t.Fatal(err)
	}
	return temp
}

// A run's event says the temps its Process's ports hold, the key's and
// the store's logs', by the directories they are of: the twin runs the
// one scan Main's runs do.
func TestARunsEventSaysTheTempsOfItsPorts(t *testing.T) {
	shared, logs := t.TempDir(), t.TempDir()
	logsDir := files.DirAt(logs, files.MustName("log"))
	twin := freshTwinOver(files.At(logsDir))
	sharedDir := files.DirAt(shared, files.MustName("lit"))
	key := plant(t, sharedDir.Path(), command.NameOf(command.PrincipalFile{}).String())
	log := plant(t, logsDir.Path(), store.LogName(twinID).String())
	twin.Process.IdentityFiles = func(git.Checkout) (files.Files, files.Files) { return files.At(sharedDir), files.NewMemory() }
	_, e := run(t, twin, "--help")
	want := []command.Leftovers{
		command.TempsBeside{Dir: command.DirOf(command.PrincipalFile{}, twinCheckout).Path(), Temps: []string{key}},
		command.TempsBeside{Dir: store.LogsDir(twinCheckout).Path(), Temps: []string{log}},
	}
	if !reflect.DeepEqual(e.Store.Temps, want) {
		t.Errorf("the event's temps are %#v; want the key's and the log's, by their directories: %#v", e.Store.Temps, want)
	}
}

// Each write's not-durable fact reaches the outcome from the value its
// call returned, whichever call made it.
func TestEveryWriteWhoseSyncFailedEndsTheRunNotDurable(t *testing.T) {
	t.Run("the checkout id", func(t *testing.T) {
		s := unsynced(t, filestest.Faults{})
		s.twin.Process.IdentityFiles = func(git.Checkout) (files.Files, files.Files) {
			return files.NewMemory(), filestest.NotDurable(files.NewMemory(), filestest.Faults{CreateOnce: errSync})
		}
		notDurableRun(t, s.lit("admin", "init"), command.PathOf(command.IDFile{}, twinCheckout))
	})
	t.Run("the founding pin", func(t *testing.T) {
		s := unsynced(t, filestest.Faults{})
		pins := filestest.NotDurable(files.NewMemory(), filestest.Faults{CreateOnce: errSync})
		s.twin.Process.Pin = func(git.Checkout) files.Files { return pins }
		notDurableRun(t, s.lit("admin", "init"), pinPath(twinCheckout))
	})
	t.Run("a requester's frame", func(t *testing.T) {
		s := unsynced(t, filestest.Faults{Append: errSync})
		s.lit("admin", "init")
		beside := NewTwinBeside(*s.twin)
		r := session{t: t, twin: &beside}.lit("admin", "init")
		notDurableRun(t, r, "a frame of")
		if !strings.Contains(r.out, "membership requested") {
			t.Errorf("the requester's init printed %q; want the request said", r.out)
		}
	})
	t.Run("a forged frame", func(t *testing.T) {
		s := unsynced(t, filestest.Faults{Append: errSync})
		s.lit("admin", "init")
		err := s.twin.Forge(verbs.Add, verbs.Operand{Path: "unfiled/children", Value: `{title: forged}`})
		var nd notDurable
		if !errors.As(err, &nd) {
			t.Errorf("Forge whose frame's sync failed = %v; want notDurable", err)
		}
	})
}

// A run that fails after a write landed not durable carries both
// failures: its own class and reason, and the write said.
func TestAFailedRunCarriesItsNotDurableWrites(t *testing.T) {
	s := unsynced(t, filestest.Faults{})
	// version one's file lands not durable, and then does not read back
	broken := unreadWorkspace{files.NewMemory()}
	s.twin.Process.WorkTree = func(git.Checkout) files.Files {
		return filestest.NotDurable(broken, filestest.Faults{CreateOnce: errSync})
	}
	r := s.lit("admin", "init")
	if r.status != 78 || reasonOf(r.event) != "workspace" || !strings.Contains(r.err, errWorkspaceUnread.Error()) ||
		!strings.Contains(r.err, filepath.Join(twinCheckout.WorkTree, verbs.WorkspacePath())+" landed") || !strings.Contains(r.err, errSync.Error()) {
		t.Errorf("the run is status %d, reason %q, stderr %q; want the readback's failure, and the workspace file not durable, both said", r.status, reasonOf(r.event), r.err)
	}
	f, _ := r.event.Outcome.(cli.Failed)
	var nd notDurable
	if !errors.As(f.Failure, &nd) || !errors.Is(f.Failure, errWorkspaceUnread) {
		t.Errorf("the run's failure %v holds no notDurable or no workspace failure", f.Failure)
	}
}

// unreadWorkspace is a work tree whose workspace file does not read back.
type unreadWorkspace struct{ files.Files }

var errWorkspaceUnread = errors.New("the workspace file does not read")

func (u unreadWorkspace) ReadAll(n files.Name) ([]byte, error) {
	if n == verbs.WorkspaceFile() {
		return nil, errWorkspaceUnread
	}
	return u.Files.ReadAll(n)
}

// gone is a stdout whose reader left.
type gone struct{}

func (gone) Write([]byte) (int, error) { return 0, syscall.EPIPE }

// A key this run created whose phrase could not be written fails the run,
// EPIPE too, which a plain answer's write takes as Done: no one saw the
// phrase, and it is shown only once.
func TestAPhraseThatCouldNotBeShownFailsTheRun(t *testing.T) {
	fresh := func(t *testing.T, shared files.Files) said {
		s := unsynced(t, filestest.Faults{})
		s.twin.Process.IdentityFiles = func(git.Checkout) (files.Files, files.Files) { return shared, files.NewMemory() }
		s.twin.Process.Out = gone{}
		return s.lit("admin", "init")
	}
	keyPath := command.PathOf(command.PrincipalFile{}, twinCheckout)
	t.Run("a key read back", func(t *testing.T) {
		r := fresh(t, files.NewMemory())
		if r.status == 0 || reasonOf(r.event) != "output" || !strings.Contains(r.err, keyPath) || !strings.Contains(r.err, "was not shown") {
			t.Errorf("init is status %d, reason %q, stderr %q; want it failed, the phrase of %s said not shown", r.status, reasonOf(r.event), r.err, keyPath)
		}
	})
	t.Run("a key that does not read back", func(t *testing.T) {
		r := fresh(t, unreadKey{files.NewMemory()})
		if r.status == 0 || reasonOf(r.event) != "output" || !strings.Contains(r.err, "was not shown") || !strings.Contains(r.err, errKeyUnread.Error()) {
			t.Errorf("init is status %d, reason %q, stderr %q; want the phrase not shown and the readback's failure, both said", r.status, reasonOf(r.event), r.err)
		}
	})
}
