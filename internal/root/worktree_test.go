//go:build integration

package root

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/verbs"
)

// initOnDisk runs admin init in a fresh twin whose work tree is c's on
// disk, through the files Main gives it, and returns its status and its
// event.
func initOnDisk(t *testing.T, c git.Checkout) (int, command.Event) {
	t.Helper()
	twin := NewFreshTwin()
	twin.Code.Commit(map[string][]byte{verbs.WorkspacePath(): assets.DefaultWorkspace.Bytes()})
	twin.Process.Locate = func() command.Located { return command.AtCheckout{Checkout: c} }
	// c's clone's store is the twin's, in memory
	twin.Process.Stores = storesOf(c, twin.Process.Stores(twinCheckout))
	twin.Process.WorkTree = workTree(files.NewDurability())
	return run(t, twin, "admin", "init")
}

// treeOnDisk is a checkout whose work tree is a fresh directory.
func treeOnDisk(t *testing.T) git.Checkout {
	tree := t.TempDir()
	dotGit := filepath.Join(tree, ".git")
	return git.Checkout{WorkTree: tree, GitDir: dotGit, CommonDir: dotGit, Format: git.SHA1}
}

// A work tree's .lit that is a symlink is refused wherever it points, and
// init writes nothing where it points: a work tree a clone checks out
// cannot steer init's writes into lit's own files or out of the tree. A
// real .lit, the control, takes the workspace file.
func TestInitRefusesAWorkTreeWhoseLitIsASymlink(t *testing.T) {
	t.Run("control: a real .lit takes the workspace file", func(t *testing.T) {
		c := treeOnDisk(t)
		if status, e := initOnDisk(t, c); status != 0 {
			t.Fatalf("admin init over a real .lit exited %d, %#v; want 0", status, e.Outcome())
		}
		if b, err := os.ReadFile(filepath.Join(c.WorkTree, verbs.LitDir().String(), verbs.WorkspaceFile().String())); err != nil || string(b) != string(assets.DefaultWorkspace.Bytes()) {
			t.Errorf("the work tree's workspace file holds %q, %v; want the default profile", b, err)
		}
	})
	for name, target := range map[string]func(*testing.T, git.Checkout) (link, dir string){
		"a link to a directory outside the work tree": func(t *testing.T, _ git.Checkout) (string, string) {
			dir := t.TempDir()
			return dir, dir
		},
		"a link to nothing outside the work tree": func(t *testing.T, _ git.Checkout) (string, string) {
			dir := t.TempDir()
			return filepath.Join(dir, "nothing"), dir
		},
		// relative, as a committed link is, to the clone's shared dir, where
		// the pin and the principal's key live
		"a link to lit's shared dir inside it": func(t *testing.T, c git.Checkout) (string, string) {
			dir := c.Shared().Path()
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			rel, err := filepath.Rel(c.WorkTree, dir)
			if err != nil {
				t.Fatal(err)
			}
			return rel, dir
		},
	} {
		t.Run(name, func(t *testing.T) {
			c := treeOnDisk(t)
			link, dir := target(t, c)
			if err := os.Symlink(link, filepath.Join(c.WorkTree, verbs.LitDir().String())); err != nil {
				t.Fatal(err)
			}
			_, e := initOnDisk(t, c)
			if err := failure(t, e); !errors.As(err, new(files.NotTheDirectory)) {
				t.Errorf("admin init over a .lit linked to %s failed with %v; want NotTheDirectory", link, err)
			}
			if es, err := os.ReadDir(dir); err != nil || len(es) != 0 {
				t.Errorf("after init the link's target %s holds %v, %v; want nothing", dir, es, err)
			}
		})
	}
}
