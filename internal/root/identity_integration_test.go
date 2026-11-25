//go:build integration

package root

import (
	"os"
	"testing"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/files"
)

func TestTheIdentityFilesOnDiskAreASecrets(t *testing.T) {
	c := worktrees(t, t.TempDir())[1]
	shared, private := identityFiles(files.NewDurability())(c)
	if _, err := identityOf(mint(shared, private, 1)); err != nil {
		t.Fatal(err)
	}
	for _, f := range []command.File{command.IDFile{}, command.PrincipalFile{}} {
		info, err := os.Stat(command.PathOf(f, c))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s is %v; want -rw-------", command.PathOf(f, c), info.Mode().Perm())
		}
	}
}

// The founding pin is no secret: on disk it is 0644, readable as the
// clone's other files are, unlike the identity files beside it.
func TestThePinOnDiskIsReadable(t *testing.T) {
	c := worktrees(t, t.TempDir())[1]
	if _, err := pin(&writes{stored: &command.StoreFacts{}}, onceFile{files: pinFiles(files.NewDurability())(c), name: pinFile}, events.EventID{0x1}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(pinPath(c))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("%s is %v; want -rw-r--r--", pinPath(c), info.Mode().Perm())
	}
}
