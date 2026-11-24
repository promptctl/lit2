package root

import (
	"errors"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/git"
)

// clone is one clone's shared identity dir and two worktrees' private
// ones, over the disk adapter or its twin.
type clone struct {
	shared   files.Files
	worktree [2]files.Files
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
	return clone{shared: shared, worktree: [2]files.Files{a, b}}
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
	fresh := func() files.Files { return files.NewMemoryMode(identityMode) }
	return clone{shared: fresh(), worktree: [2]files.Files{fresh(), fresh()}}
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
		// The window that could make two keys is the first minter's, between
		// drawing its key and that key landing in the shared dir. The first
		// is held there while the second mints whole, so the second's key
		// lands first; only a create-once that refuses an existing file, and
		// a mint that signs with what the file then holds, keep one key.
		// [LAW:no-ambient-temporal-coupling] the interleaving is forced by a
		// gate, not hoped for from two goroutines' timing.
		held := heldAtKeyCreate(c.shared)
		var got [2]command.Minted
		var made [2]command.Fresh
		first := make(chan error, 1)
		go func() {
			var err error
			got[0], made[0], err = MintIdentity(held, c.worktree[0], entropy(0))
			first <- err
		}()
		// a first mint that ends before the gate, failed or not, never
		// reaches it, so the test fails with its error and does not wait
		select {
		case <-held.reached:
		case err := <-first:
			t.Fatalf("the first mint ended before its key landed, with %v; want it held at the gate", err)
		}
		var err error
		if got[1], made[1], err = MintIdentity(c.shared, c.worktree[1], entropy(1)); err != nil {
			t.Fatalf("the second mint: %v", err)
		}
		close(held.release)
		if err := <-first; err != nil {
			t.Fatalf("the first mint: %v", err)
		}
		if got[0].Key.Principal() != got[1].Key.Principal() || got[0].Checkout == got[1].Checkout {
			t.Errorf("two worktrees minted %v and %v; want one principal and two checkout ids", got[0], got[1])
		}
		if made[0] != (command.Fresh{Checkout: files.Created, Key: files.Existing}) || made[1] != (command.Fresh{Checkout: files.Created, Key: files.Created}) {
			t.Errorf("the held mint made %+v and the other %+v; want the other's key created, the held one's found, and each its own checkout id", made[0], made[1])
		}
	})
	t.Run("the identity files are a secret's, -rw-------", func(t *testing.T) {
		c := fresh(t)
		if _, _, err := MintIdentity(c.shared, c.worktree[0], entropy(1)); err != nil {
			t.Fatal(err)
		}
		for _, f := range []struct {
			fs   files.Files
			file command.File
		}{{c.shared, command.PrincipalFile{}}, {c.worktree[0], command.IDFile{}}} {
			moded, ok := f.fs.(filestest.ModedFiles)
			if !ok {
				t.Fatalf("%T says no file's mode", f.fs)
			}
			if mode, err := moded.Mode(command.NameOf(f.file)); err != nil || mode != 0o600 {
				t.Errorf("%s is %v, %v; want -rw-------", command.NameOf(f.file), mode, err)
			}
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
		if made != (command.Fresh{Checkout: files.Existing, Key: files.Existing}) {
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
		if _, _, err := MintIdentity(damaged.shared, damaged.worktree[0], entropy(1)); !errors.Is(err, errIdentityUnreadable) {
			t.Errorf("minting over a damaged key file = %v; want errIdentityUnreadable", err)
		}
		if got, ok := readIdentity(damaged.shared, damaged.worktree[0]).(command.Unreadable); !ok || got.File != (command.PrincipalFile{}) {
			t.Errorf("a damaged key file reads as %#v; want it unreadable", got)
		}
	})
}

// heldFiles is a Files that holds the one CreateOnce of the key file before
// it reaches the files beneath: it says so on reached, then waits for
// release.
type heldFiles struct {
	files.Files
	reached, release chan struct{}
}

func heldAtKeyCreate(fs files.Files) heldFiles {
	return heldFiles{Files: fs, reached: make(chan struct{}), release: make(chan struct{})}
}

func (h heldFiles) CreateOnce(p files.Name, b []byte) (files.Made, error) {
	if p == command.NameOf(command.PrincipalFile{}) {
		close(h.reached)
		<-h.release
	}
	return h.Files.CreateOnce(p, b)
}
