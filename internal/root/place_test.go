//go:build integration

package root

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
)

// minted is the twin's identity, as its two files hold it.
var minted = map[command.File]string{command.IDFile{}: string(twinID.FileText()), command.PrincipalFile{}: string(twinKey().FileText())}

// checkout makes a main worktree in a temporary directory, at its physical
// path, holding each identity file given with its text and no other.
func checkout(t *testing.T, files map[command.File]string) string {
	t.Helper()
	dir := gittest.Physical(t)
	gittest.Git(t, dir, "init", "-q")
	c := git.Checkout{WorkTree: dir, GitDir: filepath.Join(dir, ".git"), CommonDir: filepath.Join(dir, ".git")}
	for f, text := range files {
		path := command.PathOf(f, c)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func identityAt(t *testing.T, dir string) command.Identity {
	t.Helper()
	in, ok := place(gittest.Runner(t), dir, files.NewDurability()).(command.InCheckout)
	if !ok {
		t.Fatalf("%s is not in a checkout: %#v", dir, place(gittest.Runner(t), dir, files.NewDurability()))
	}
	return in.Identity
}

func TestTheIdentityFilesGiveTheWriterAndItsKey(t *testing.T) {
	got, ok := identityAt(t, checkout(t, minted)).(command.Minted)
	if !ok || got.Checkout != twinID || got.Key.Principal() != twinKey().Principal() {
		t.Errorf("the identity is %#v; want Minted %s, signing as %s", got, twinID, twinKey())
	}
}

func TestAMissingFileIsUnmintedAndGarbageIsUnreadable(t *testing.T) {
	for name, tc := range map[string]struct {
		files map[command.File]string
		want  command.Identity
	}{
		"no files":        {nil, command.Unminted{Missing: command.Both{}}},
		"no id file":      {map[command.File]string{command.PrincipalFile{}: minted[command.PrincipalFile{}]}, command.Unminted{Missing: command.Only{File: command.IDFile{}}}},
		"no key file":     {map[command.File]string{command.IDFile{}: minted[command.IDFile{}]}, command.Unminted{Missing: command.Only{File: command.PrincipalFile{}}}},
		"a garbage id":    {map[command.File]string{command.IDFile{}: "not an id\n"}, command.Unreadable{File: command.IDFile{}}},
		"a damaged key":   {map[command.File]string{command.IDFile{}: minted[command.IDFile{}], command.PrincipalFile{}: "ed25519 short\n"}, command.Unreadable{File: command.PrincipalFile{}}},
		"both unreadable": {map[command.File]string{command.IDFile{}: "x", command.PrincipalFile{}: "y"}, command.Unreadable{File: command.IDFile{}}},
	} {
		got := identityAt(t, checkout(t, tc.files))
		if u, ok := got.(command.Unreadable); ok {
			if want, ok := tc.want.(command.Unreadable); !ok || u.File != want.File || u.Err == nil {
				t.Errorf("%s: the identity is %#v; want %#v and why", name, got, tc.want)
			}
			continue
		}
		if got != tc.want {
			t.Errorf("%s: the identity is %#v; want %#v", name, got, tc.want)
		}
	}
}

func TestACwdReachedThroughASymlinkFindsItsCheckout(t *testing.T) {
	dir := checkout(t, nil)
	sub, link := filepath.Join(dir, "sub"), filepath.Join(t.TempDir(), "link")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sub, link); err != nil {
		t.Fatal(err)
	}
	if in, ok := place(gittest.Runner(t), link, files.NewDurability()).(command.InCheckout); !ok || in.Checkout.WorkTree != dir {
		t.Errorf("place(%s), a link to %s, = %#v; want the checkout at %s", link, sub, place(gittest.Runner(t), link, files.NewDurability()), dir)
	}
}

func TestOutsideACheckoutIsNoCheckout(t *testing.T) {
	dir, ceilingEnv := gittest.Outside(t)
	if got := place(gittest.Runner(t, ceilingEnv), dir, files.NewDurability()); got != (command.NoCheckout{Dir: dir}) {
		t.Errorf("place(%s) = %#v; want NoCheckout", dir, got)
	}
}

func TestAnUnwritableLogIsSaidAndTheStatusStands(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	twin := NewTwin()
	twin.Process.Commands = command.NewLog(filepath.Join(file, "commands"))
	if status := Run(twin.Process, []string{"--help"}); status != 0 {
		t.Errorf("--help with an unwritable log exited %d; want 0", status)
	}
	if !strings.HasPrefix(twin.Err.String(), "lit2: command event not recorded: ") {
		t.Errorf("stderr is %q; want the unrecorded line", twin.Err)
	}
	status := Run(twin.Process, []string{"frobnicate"})
	if status != 64 || !strings.Contains(twin.Err.String(), "error (reason=unknown_verb)") || strings.Count(twin.Err.String(), "not recorded") != 2 {
		t.Errorf("frobnicate with an unwritable log: status %d, stderr %q", status, twin.Err)
	}
}

func TestAnUnremovableOldDayFileStillLeavesTheRecord(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root removes files from a directory whatever its mode")
	}
	dir := t.TempDir()
	// a directory named as a day file is not one, so pruning passes it by;
	// something in it would make its removal fail
	if err := os.MkdirAll(filepath.Join(dir, "2026-09-02.jsonl", "held"), 0o700); err != nil {
		t.Fatal(err)
	}
	twin := NewTwin()
	twin.Process.Commands = command.NewLog(dir)
	if status := Run(twin.Process, []string{"--help"}); status != 0 || twin.Err.Len() != 0 {
		t.Fatalf("--help beside a directory named as an old day file: status %d, stderr %q", status, twin.Err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-09-01.jsonl"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// today's file takes the append; the old one cannot be removed
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Error(err)
		}
	})
	if status := Run(twin.Process, []string{"--help"}); status != 0 || !strings.HasPrefix(twin.Err.String(), "lit2: command log: pruning failed: ") {
		t.Errorf("--help with an unremovable old day file: status %d, stderr %q", status, twin.Err)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "2026-11-11.jsonl")); err != nil || strings.Count(string(b), "\n") != 2 {
		t.Errorf("the log holds %q, %v; want both lines", b, err)
	}
}

func TestARelativeXDGStateHomeCountsAsUnset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_STATE_HOME", "relative-state")
	twin := NewTwin()
	twin.Process.Commands = commandLog()
	if status := Run(twin.Process, []string{"--help"}); status != 0 || twin.Err.Len() != 0 {
		t.Fatalf("--help: status %d, stderr %q", status, twin.Err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "state", "lit", "commands", "2026-11-11.jsonl")); err != nil {
		t.Errorf("the event is not under ~/.local/state: %v", err)
	}
	if _, err := os.Stat("relative-state"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a relative XDG_STATE_HOME was used: %v", err)
	}
}
