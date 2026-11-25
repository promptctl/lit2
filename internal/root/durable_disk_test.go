//go:build integration

package root

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

// Every command in a checkout says, for each directory of its own files,
// listed once however many it holds, the temps no create has reaped, a
// crash's, or that it was not scanned, and why; and the same of its
// store's logs. Never relying on git to show them.
func TestLeftoversSaysTheCheckoutsTemps(t *testing.T) {
	dir := checkout(t, minted)
	c := git.Checkout{WorkTree: dir, GitDir: filepath.Join(dir, ".git"), CommonDir: filepath.Join(dir, ".git")}
	u := files.NewDurability()
	// the logs on disk, under the clone's lit repository held in memory:
	// listing them reads only the directory
	stores := func(c git.Checkout) store.Ports {
		return store.RepositoryInMemory(files.At(store.LogsDir(c), u), storetest.Entropy(1))
	}
	disk := Process{IdentityFiles: identityFiles(u), Pin: pinFiles(u), WorkTree: workTree(u), Stores: stores}
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
	// the logs are the clone's, every writer's, so they are scanned in a
	// checkout not yet minted too
	logs := plant(t, store.LogsDir(c).Path(), "another writer's log")
	want = append(want, command.TempsScanned{Dir: store.LogsDir(c).Path(), Temps: []string{logs}})
	if got := leftovers(disk, command.InCheckout{Checkout: c}); !reflect.DeepEqual(got, want) {
		t.Errorf("the checkout's temps are %#v; want each directory's temps, once, and the logs': %#v", got, want)
	}
	t.Run("a .lit that is no directory is not scanned, once", func(t *testing.T) {
		if err := os.RemoveAll(litDir(c).Path()); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(t.TempDir(), litDir(c).Path()); err != nil {
			t.Fatal(err)
		}
		got := leftovers(disk, command.InCheckout{Checkout: c})
		if len(got) != len(want) {
			t.Fatalf("the checkout's temps are %#v; want one row per directory, as before", got)
		}
		lit := got[at[litDir(c).Path()]]
		if u, ok := lit.(command.TempsNotScanned); !ok || u.Dir != litDir(c).Path() || !errors.As(u.Why, new(files.NotTheDirectory)) {
			t.Errorf("the .lit temps are %#v; want not scanned, the symlink refused", lit)
		}
	})
	t.Run("outside a checkout no directory is scanned", func(t *testing.T) {
		if got := leftovers(disk, command.NoCheckout{Dir: dir}); got != nil {
			t.Errorf("outside a checkout the temps are %#v; want none", got)
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
