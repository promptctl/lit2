package root

import (
	"errors"
	"fmt"
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
	return sessionOver(t, filestest.NotDurable(files.NewMemory(store.LogsDir(twinCheckout)), faults))
}

// sessionOver is a session over a fresh twin whose store's logs are logs,
// its workspace committed as admin init applies it.
func sessionOver(t *testing.T, logs files.Files) session {
	twin := freshTwinOver(logs)
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
		under, private := files.NewMemory(twinShared), files.NewMemory(twinPrivate)
		s.twin.Process.IdentityFiles = func(git.Checkout) (files.Files, files.Files) { return shared(under), private }
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
	tree := files.NewMemory(litDir(twinCheckout))
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

// twinShared and twinPrivate are the directories of the twin's identity
// files, which a port a test gives it is of, so its paths are the twin's.
var (
	twinShared  = command.DirOf(command.PrincipalFile{}, twinCheckout)
	twinPrivate = command.DirOf(command.IDFile{}, twinCheckout)
)

// A run's not-durable failure keeps each write's failed sync in its
// chain, so a caller asking for the errno finds it, whichever write's it
// was and whatever failure the run ended in first.
func TestANotDurableRunsChainHoldsEachSyncsError(t *testing.T) {
	w := writes{stored: &command.StoreFacts{}}
	w.created(command.Creation{Path: "/twin/.lit/founding", Placing: files.PlacedNotDurable{Why: fmt.Errorf("syncing: %w", syscall.EIO)}})
	if err := w.appended(store.AppendedNotDurable{Written: store.Written{Records: 1}, Log: store.LogLookedUp{}, Why: syscall.ENOSPC}); err != nil {
		t.Fatal(err)
	}
	for name, o := range map[string]cli.Outcome{"done": durable(*w.stored, cli.Done{}), "failed first": durable(*w.stored, failed(errors.New("the body failed")))} {
		f, ok := o.(cli.Failed)
		if !ok || !errors.Is(f.Failure, syscall.EIO) || !errors.Is(f.Failure, syscall.ENOSPC) {
			t.Errorf("a %s run is %#v; want failed, its chain holding EIO and ENOSPC", name, o)
		}
	}
}

// An append that landed no frame after its log's name's sync failed
// carries that failed sync into the run's outcome, beside its own
// failure, not only onto the event.
func TestAnAppendThatLandedNothingCarriesItsLogsFailedSync(t *testing.T) {
	w := writes{stored: &command.StoreFacts{}}
	errRead := errors.New("the log's header did not read")
	err := w.appended(store.NotAppended{Log: store.LogNotDurable{Log: store.LogCreated{}, Why: syscall.EIO}, Why: errRead})
	if err == nil {
		t.Fatal("an append that landed nothing answered no error")
	}
	f, ok := durable(*w.stored, failed(err)).(cli.Failed)
	if !ok || !errors.Is(f.Failure, syscall.EIO) || !strings.Contains(f.Failure.Error(), errRead.Error()) {
		t.Errorf("the run is %#v; want failed by the append's failure, its chain holding the log's EIO", f)
	}
	if reason := f.Failure.Reason(); reason == "durability" {
		t.Errorf("the run's reason is %q; want the append's own failure first", reason)
	}
}

// A body that panics still ends: its writes' durability is judged, and a
// temps scan that panics in turn is said as a scan that broke, never an
// empty scan read as nothing left, and never in place of the body's panic.
func TestAPanickingBodyStillEnds(t *testing.T) {
	s := sessionOver(t, files.NewMemory(store.LogsDir(twinCheckout)))
	s.twin.Process.WorkTree = func(git.Checkout) files.Files { panic("the work tree is gone") }
	r := s.lit("admin", "init")
	f, ok := r.event.Outcome().(cli.Failed)
	var in command.Internal
	if !ok || !errors.As(f.Failure, &in) || !strings.Contains(in.Error(), "the work tree is gone") {
		t.Errorf("the panicking run's outcome is %#v; want the body's panic", r.event.Outcome())
	}
	if len(r.event.Store.Temps) != 1 {
		t.Fatalf("the panicking run's temps are %#v; want the scan that broke", r.event.Store.Temps)
	}
	if broke, isBroke := r.event.Store.Temps[0].(command.TempsScanBroke); !isBroke || !strings.Contains(broke.Broke.Error(), "the work tree is gone") || len(broke.Broke.Stack()) == 0 {
		t.Errorf("the panicking run's temps are %#v; want the scan that broke, with its panic", r.event.Store.Temps)
	}
}

// Each write's not-durable fact reaches the outcome from the value its
// call returned, whichever call made it.
func TestEveryWriteWhoseSyncFailedEndsTheRunNotDurable(t *testing.T) {
	t.Run("the checkout id", func(t *testing.T) {
		s := unsynced(t, filestest.Faults{})
		shared, private := files.NewMemory(twinShared), filestest.NotDurable(files.NewMemory(twinPrivate), filestest.Faults{CreateOnce: errSync})
		s.twin.Process.IdentityFiles = func(git.Checkout) (files.Files, files.Files) { return shared, private }
		notDurableRun(t, s.lit("admin", "init"), command.PathOf(command.IDFile{}, twinCheckout))
	})
	t.Run("the founding pin", func(t *testing.T) {
		s := unsynced(t, filestest.Faults{})
		pins := filestest.NotDurable(files.NewMemory(pinDir(twinCheckout)), filestest.Faults{CreateOnce: errSync})
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
	broken := unreadWorkspace{files.NewMemory(litDir(twinCheckout))}
	s.twin.Process.WorkTree = func(git.Checkout) files.Files {
		return filestest.NotDurable(broken, filestest.Faults{CreateOnce: errSync})
	}
	r := s.lit("admin", "init")
	if r.status != 78 || reasonOf(r.event) != "workspace" || !strings.Contains(r.err, errWorkspaceUnread.Error()) ||
		!strings.Contains(r.err, filepath.Join(twinCheckout.WorkTree, verbs.WorkspacePath())+" landed") || !strings.Contains(r.err, errSync.Error()) {
		t.Errorf("the run is status %d, reason %q, stderr %q; want the readback's failure, and the workspace file not durable, both said", r.status, reasonOf(r.event), r.err)
	}
	f, _ := r.event.Outcome().(cli.Failed)
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
		private := files.NewMemory(twinPrivate)
		s.twin.Process.IdentityFiles = func(git.Checkout) (files.Files, files.Files) { return shared, private }
		s.twin.Process.Out = gone{}
		return s.lit("admin", "init")
	}
	keyPath := command.PathOf(command.PrincipalFile{}, twinCheckout)
	t.Run("a key read back", func(t *testing.T) {
		r := fresh(t, files.NewMemory(twinShared))
		if r.status == 0 || reasonOf(r.event) != "output" || !strings.Contains(r.err, keyPath) || !strings.Contains(r.err, "was not shown") {
			t.Errorf("init is status %d, reason %q, stderr %q; want it failed, the phrase of %s said not shown", r.status, reasonOf(r.event), r.err, keyPath)
		}
	})
	t.Run("a key that does not read back", func(t *testing.T) {
		r := fresh(t, unreadKey{files.NewMemory(twinShared)})
		if r.status == 0 || reasonOf(r.event) != "output" || !strings.Contains(r.err, "was not shown") || !strings.Contains(r.err, errKeyUnread.Error()) {
			t.Errorf("init is status %d, reason %q, stderr %q; want the phrase not shown and the readback's failure, both said", r.status, reasonOf(r.event), r.err)
		}
	})
}
