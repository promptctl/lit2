//go:build integration

package root

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
)

// worktrees is a main worktree and a linked one of one clone, at root,
// their directories made as git makes them: a create never makes the
// directory its Dir is based in.
func worktrees(t *testing.T, root string) [2]git.Checkout {
	common := filepath.Join(root, "main", ".git")
	cs := [2]git.Checkout{
		{WorkTree: filepath.Join(root, "main"), GitDir: common, CommonDir: common},
		{WorkTree: filepath.Join(root, "linked"), GitDir: filepath.Join(common, "worktrees", "linked"), CommonDir: common},
	}
	for _, c := range cs {
		for _, d := range []string{c.WorkTree, c.GitDir} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	return cs
}

func onDisk(t *testing.T) clone {
	cs := worktrees(t, t.TempDir())
	u := files.NewDurability()
	shared, a := identityFiles(u)(cs[0])
	_, b := identityFiles(u)(cs[1])
	return clone{shared: shared, worktree: [2]files.Files{a, b}}
}

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

// TestIdentityOnDisk is TestIdentity over the identity files on disk, as
// Main's ports place them in two worktrees of one clone.
func TestIdentityOnDisk(t *testing.T) { identityContract(t, onDisk) }

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
