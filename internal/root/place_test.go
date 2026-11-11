package root

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/store"
)

// checkout makes a main worktree in a temporary directory, its id file
// holding id, or no id file when id is empty.
func checkout(t *testing.T, id string) (dir, idPath string) {
	t.Helper()
	dir = t.TempDir()
	private := filepath.Join(dir, ".git", "lit", "checkout")
	if err := os.MkdirAll(private, 0o700); err != nil {
		t.Fatal(err)
	}
	idPath = filepath.Join(private, "id")
	if id != "" {
		if err := os.WriteFile(idPath, []byte(id), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, idPath
}

func writerAt(t *testing.T, dir string) command.Writer {
	t.Helper()
	in, ok := place(dir).(command.InCheckout)
	if !ok {
		t.Fatalf("%s is not in a checkout: %#v", dir, place(dir))
	}
	return in.Writer
}

func TestTheIDFileGivesTheWriter(t *testing.T) {
	const hex = "7a1e0f7b112d4c889e015a6b3c2f4099\n"
	dir, _ := checkout(t, hex)
	want, err := store.ParseCheckoutID(strings.TrimSpace(hex))
	if err != nil {
		t.Fatal(err)
	}
	if got := writerAt(t, dir); got != (command.Minted{ID: want}) {
		t.Errorf("the writer is %#v; want Minted %s", got, want)
	}
}

func TestNoIDFileIsUnmintedAndGarbageIsUnreadable(t *testing.T) {
	dir, idPath := checkout(t, "")
	if got := writerAt(t, dir); got != (command.Unminted{Path: idPath}) {
		t.Errorf("with no id file the writer is %#v; want Unminted at %s", got, idPath)
	}
	dir, idPath = checkout(t, "not an id\n")
	if got, ok := writerAt(t, dir).(command.Unreadable); !ok || got.Path != idPath {
		t.Errorf("with a garbage id file the writer is %#v; want Unreadable at %s", got, idPath)
	}
}

func TestOutsideACheckoutIsNoCheckout(t *testing.T) {
	dir := t.TempDir()
	if got := place(dir); got != (command.NoCheckout{Dir: dir}) {
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
