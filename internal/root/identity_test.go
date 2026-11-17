package root

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// clone is one clone's shared identity dir and two worktrees' private
// ones, over the disk adapter or its twin.
type clone struct {
	shared   store.Files
	worktree [2]store.Files
}

// worktrees is a main worktree and a linked one of one clone, at root.
func worktrees(root string) [2]git.Checkout {
	common := filepath.Join(root, "main", ".git")
	return [2]git.Checkout{
		{WorkTree: filepath.Join(root, "main"), GitDir: common, CommonDir: common},
		{WorkTree: filepath.Join(root, "linked"), GitDir: filepath.Join(common, "worktrees", "linked"), CommonDir: common},
	}
}

func onDisk(t *testing.T) clone {
	cs := worktrees(t.TempDir())
	shared, a := identityFiles(cs[0])
	_, b := identityFiles(cs[1])
	return clone{shared: shared, worktree: [2]store.Files{a, b}}
}

func TestTheIdentityFilesOnDiskAreASecrets(t *testing.T) {
	c := worktrees(t.TempDir())[1]
	shared, private := identityFiles(c)
	if _, _, err := MintIdentity(shared, private, entropy(1)); err != nil {
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

func inMemory(*testing.T) clone {
	return clone{shared: store.NewMemoryFiles(), worktree: [2]store.Files{store.NewMemoryFiles(), store.NewMemoryFiles()}}
}

func entropy(seed byte) *rand.ChaCha8 { return rand.NewChaCha8([32]byte{'i', 'd', seed}) }

func TestIdentity(t *testing.T) {
	for name, fresh := range map[string]func(*testing.T) clone{"disk": onDisk, "twin": inMemory} {
		t.Run(name, func(t *testing.T) { identityContract(t, fresh) })
	}
}

func identityContract(t *testing.T, fresh func(*testing.T) clone) {
	t.Run("two worktrees minting at once sign with one key", func(t *testing.T) {
		c := fresh(t)
		var got [2]command.Minted
		var made [2]command.Fresh
		var wg sync.WaitGroup
		for i := range 2 {
			wg.Go(func() {
				m, f, err := MintIdentity(c.shared, c.worktree[i], entropy(byte(i)))
				if err != nil {
					t.Error(err)
				}
				got[i], made[i] = m, f
			})
		}
		wg.Wait()
		if got[0].Key.Principal() != got[1].Key.Principal() || got[0].Checkout == got[1].Checkout {
			t.Errorf("two worktrees minted %v and %v; want one principal and two checkout ids", got[0], got[1])
		}
		if (made[0].Key == store.Created) == (made[1].Key == store.Created) || made[0].Checkout != store.Created || made[1].Checkout != store.Created {
			t.Errorf("the two mints made %+v and %+v; want one key created, and each its own checkout id", made[0], made[1])
		}
	})
	t.Run("a second mint returns the first identity", func(t *testing.T) {
		c := fresh(t)
		first, _, err := MintIdentity(c.shared, c.worktree[0], entropy(1))
		if err != nil {
			t.Fatal(err)
		}
		again, made, err := MintIdentity(c.shared, c.worktree[0], entropy(2))
		if err != nil || again.Checkout != first.Checkout || again.Key.Principal() != first.Key.Principal() {
			t.Errorf("a second mint is %v, %v; want the first, %v", again, err, first)
		}
		if made != (command.Fresh{Checkout: store.Existing, Key: store.Existing}) {
			t.Errorf("a second mint made %+v; want both files existing", made)
		}
		if read := readIdentity(c.shared, c.worktree[0]); read != command.Identity(first) {
			t.Errorf("the files read as %#v; want the identity minted, %#v", read, first)
		}
	})
	t.Run("a damaged key is unreadable and an absent id unminted", func(t *testing.T) {
		c := fresh(t)
		if _, _, err := MintIdentity(c.shared, c.worktree[0], entropy(1)); err != nil {
			t.Fatal(err)
		}
		if got := readIdentity(c.shared, c.worktree[1]); got != (command.Unminted{Missing: command.Only{File: command.IDFile{}}}) {
			t.Errorf("a worktree with no id file reads as %#v; want its id file missing", got)
		}
		damaged := fresh(t)
		if _, err := damaged.shared.CreateOnce(command.NameOf(command.PrincipalFile{}), []byte("ed25519 cut short\n")); err != nil {
			t.Fatal(err)
		}
		if _, _, err := MintIdentity(damaged.shared, damaged.worktree[0], entropy(1)); err == nil {
			t.Error("minting over a damaged key file succeeded; want it refused")
		}
		if got, ok := readIdentity(damaged.shared, damaged.worktree[0]).(command.Unreadable); !ok || got.File != (command.PrincipalFile{}) {
			t.Errorf("a damaged key file reads as %#v; want it unreadable", got)
		}
	})
}
