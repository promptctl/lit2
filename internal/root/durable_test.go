package root

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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
		under := files.NewMemory(twinShared)
		s.twin.Process.IdentityFiles = func(git.Checkout) (files.Files, files.Files) { return shared(under), files.NewMemory(twinPrivate) }
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

// Every command in a checkout says, for each directory of its own files,
// listed once however many it holds, the temps no create has reaped, a
// crash's, or that it was not scanned, and why; and the same of its
// store's logs. Never relying on git to show them.
func TestLeftoversSaysTheCheckoutsTemps(t *testing.T) {
	dir := checkout(t, minted)
	c := git.Checkout{WorkTree: dir, GitDir: filepath.Join(dir, ".git"), CommonDir: filepath.Join(dir, ".git")}
	u := files.NewDurability()
	disk := Process{IdentityFiles: identityFiles(u), Pin: pinFiles(u), WorkTree: workTree(u)}
	unminted := command.Unopened{Why: errors.New("unminted")}
	var want []command.Leftovers
	at := map[string]int{}
	for _, f := range disk.onceFiles(c).all() {
		d := f.files.Dir().Path()
		temp := plant(t, d, f.name.String())
		if i, seen := at[d]; seen {
			s := want[i].(command.TempsScanned)
			s.Temps = append(s.Temps, temp)
			slices.Sort(s.Temps)
			want[i] = s
			continue
		}
		at[d] = len(want)
		want = append(want, command.TempsScanned{Dir: d, Temps: []string{temp}})
	}
	if len(want) == len(disk.onceFiles(c).all()) {
		t.Fatalf("the checkout's once files are in %d directories; want some sharing one, to show it listed once", len(want))
	}
	want = append(want, command.TempsNotScanned{Dir: store.LogsDir(c).Path(), Why: unminted.Why})
	if got := leftovers(disk, command.InCheckout{Checkout: c}, &command.StoreFacts{Opening: unminted}); !reflect.DeepEqual(got, want) {
		t.Errorf("the checkout's temps are %#v; want each directory's temps, once, and the logs not scanned: %#v", got, want)
	}
	t.Run("a .lit that is no directory is not scanned, once", func(t *testing.T) {
		if err := os.RemoveAll(litDir(c).Path()); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), litDir(c).Path()); err != nil {
			t.Fatal(err)
		}
		got := leftovers(disk, command.InCheckout{Checkout: c}, &command.StoreFacts{Opening: unminted})
		if len(got) != len(want) {
			t.Fatalf("the checkout's temps are %#v; want one row per directory, as before", got)
		}
		lit := got[at[litDir(c).Path()]]
		if u, ok := lit.(command.TempsNotScanned); !ok || u.Dir != litDir(c).Path() || !errors.As(u.Why, new(files.NotTheDirectory)) {
			t.Errorf("the .lit temps are %#v; want not scanned, the symlink refused", lit)
		}
	})
	t.Run("outside a checkout no directory is scanned", func(t *testing.T) {
		if got := leftovers(disk, command.NoCheckout{Dir: dir}, &command.StoreFacts{Opening: unminted}); got != nil {
			t.Errorf("outside a checkout the temps are %#v; want none", got)
		}
	})
}

// twinShared and twinPrivate are the directories of the twin's identity
// files, which a port a test gives it is of, so its paths are the twin's.
var (
	twinShared  = command.DirOf(command.PrincipalFile{}, twinCheckout)
	twinPrivate = command.DirOf(command.IDFile{}, twinCheckout)
)

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
// the store's logs', by the directories the ports are of, each listed
// once the run's work is done: the twin runs the one scan Main's runs do.
// A directory with no temp is said scanned and empty.
func TestARunsEventSaysTheTempsOfItsPorts(t *testing.T) {
	shared, logs := t.TempDir(), t.TempDir()
	u := files.NewDurability()
	logsDir := files.DirAt(logs, files.MustName("log"))
	twin := freshTwinOver(files.At(logsDir, u))
	sharedDir := files.DirAt(shared, files.MustName("lit"))
	key := plant(t, sharedDir.Path(), command.NameOf(command.PrincipalFile{}).String())
	log := plant(t, logsDir.Path(), store.LogName(twinID).String())
	private := filestest.Memory()
	twin.Process.IdentityFiles = func(git.Checkout) (files.Files, files.Files) { return files.At(sharedDir, u), private }
	_, e := run(t, twin, "get", "lit-abc/title")
	want := []command.Leftovers{
		command.TempsScanned{Dir: private.Dir().Path()},
		command.TempsScanned{Dir: sharedDir.Path(), Temps: []string{key}},
		command.TempsScanned{Dir: pinDir(twinCheckout).Path()},
		command.TempsScanned{Dir: litDir(twinCheckout).Path()},
		command.TempsScanned{Dir: logsDir.Path(), Temps: []string{log}},
	}
	if !reflect.DeepEqual(e.Store.Temps, want) {
		t.Errorf("the event's temps are %#v; want each port's, by its own directory: %#v", e.Store.Temps, want)
	}
}

// A log temp the run's own create reaped is said as reaped, and not as
// left: the logs are listed after the append, never from a listing taken
// before it.
func TestALogTempTheRunReapedIsNotSaidAsLeft(t *testing.T) {
	logsDir := files.DirAt(t.TempDir(), files.MustName("log"))
	s := sessionOver(t, files.At(logsDir, files.NewDurability()))
	temp := plant(t, logsDir.Path(), store.LogName(twinID).String())
	r := s.lit("admin", "init")
	if len(r.event.Store.Appends) == 0 {
		t.Fatalf("init appended nothing: %+v", r.event.Store)
	}
	a, ok := r.event.Store.Appends[0].(store.Appended)
	c, created := a.Log.(store.LogCreated)
	if !ok || !created || !slices.Equal(c.Reap.Reaped, []string{temp}) {
		t.Fatalf("init's first append is %#v; want Appended, its log created, reaping %s", r.event.Store.Appends[0], temp)
	}
	logs := r.event.Store.Temps[len(r.event.Store.Temps)-1]
	if got, ok := logs.(command.TempsScanned); !ok || got.Dir != logsDir.Path() || len(got.Temps) != 0 {
		t.Errorf("the logs' temps are %#v; want %s scanned, %s not left", logs, logsDir.Path(), temp)
	}
}

// A run's not-durable failure keeps each write's failed sync in its
// chain, so a caller asking for the errno finds it, whichever write's it
// was and whatever failure the run ended in first.
func TestANotDurableRunsChainHoldsEachSyncsError(t *testing.T) {
	w := &writes{stored: &command.StoreFacts{}}
	w.created(command.Creation{Path: "/twin/.lit/founding", Placing: files.PlacedNotDurable{Why: fmt.Errorf("syncing: %w", syscall.EIO)}})
	if err := w.appended(store.AppendedNotDurable{Written: store.Written{Records: 1}, Log: store.LogLookedUp{}, Why: syscall.ENOSPC}); err != nil {
		t.Fatal(err)
	}
	for name, o := range map[string]cli.Outcome{"done": w.end(cli.Done{}), "failed first": w.end(failed(errors.New("the body failed")))} {
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
	w := &writes{stored: &command.StoreFacts{}}
	errRead := errors.New("the log's header did not read")
	err := w.appended(store.NotAppended{Log: store.LogNotDurable{Log: store.LogCreated{}, Why: syscall.EIO}, Why: errRead})
	if err == nil {
		t.Fatal("an append that landed nothing answered no error")
	}
	f, ok := w.end(failed(err)).(cli.Failed)
	if !ok || !errors.Is(f.Failure, syscall.EIO) || !strings.Contains(f.Failure.Error(), errRead.Error()) {
		t.Errorf("the run is %#v; want failed by the append's failure, its chain holding the log's EIO", f)
	}
	if reason := f.Failure.Reason(); reason == "durability" {
		t.Errorf("the run's reason is %q; want the append's own failure first", reason)
	}
}

// A body that panics still leaves its event, which says the run's end
// steps did not run, its writes' durability unjudged and its temps
// unscanned, never an empty scan read as nothing left.
func TestAPanickingBodysEventSaysItsEndStepsDidNotRun(t *testing.T) {
	s := sessionOver(t, files.NewMemory(store.LogsDir(twinCheckout)))
	s.twin.Process.WorkTree = func(git.Checkout) files.Files { panic("the work tree is gone") }
	r := s.lit("admin", "init")
	f, ok := r.event.Outcome.(cli.Failed)
	if !ok || !strings.Contains(f.Failure.Error(), "the work tree is gone") || !strings.Contains(f.Failure.Error(), "no directory's temps were scanned") ||
		!strings.Contains(f.Failure.Error(), "durability of its writes was not judged") {
		t.Errorf("the panicking run's outcome is %#v; want the panic, and that its end steps did not run", r.event.Outcome)
	}
	if r.event.Store.Temps != nil {
		t.Errorf("the panicking run's temps are %#v; want none scanned", r.event.Store.Temps)
	}
}

// Each write's not-durable fact reaches the outcome from the value its
// call returned, whichever call made it.
func TestEveryWriteWhoseSyncFailedEndsTheRunNotDurable(t *testing.T) {
	t.Run("the checkout id", func(t *testing.T) {
		s := unsynced(t, filestest.Faults{})
		s.twin.Process.IdentityFiles = func(git.Checkout) (files.Files, files.Files) {
			return files.NewMemory(twinShared), filestest.NotDurable(files.NewMemory(twinPrivate), filestest.Faults{CreateOnce: errSync})
		}
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
		s.twin.Process.IdentityFiles = func(git.Checkout) (files.Files, files.Files) { return shared, files.NewMemory(twinPrivate) }
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
