package root

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/events"
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
	shared, a := identityFiles(cs[0])
	_, b := identityFiles(cs[1])
	return clone{shared: shared, worktree: [2]files.Files{a, b}}
}

func TestTheIdentityFilesOnDiskAreASecrets(t *testing.T) {
	c := worktrees(t, t.TempDir())[1]
	shared, private := identityFiles(c)
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

func inMemory(*testing.T) clone {
	fresh := func() files.Files { return files.NewMemory() }
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
		first := make(chan Minting, 1)
		go func() {
			first <- MintIdentity(onceIn(c.worktree[0], command.IDFile{}), onceIn(held, command.PrincipalFile{}), entropy(0))
		}()
		// a first mint that ends before the gate, failed or not, never
		// reaches it, so the test fails with what it did and does not wait
		select {
		case <-held.reached:
		case m := <-first:
			t.Fatalf("the first mint ended before its key landed, as %#v; want it held at the gate", m)
		}
		second := mint(c.shared, c.worktree[1], 1)
		close(held.release)
		heldMint := <-first
		found, isFound := heldMint.(Found)
		made, isMade := second.(Made)
		if !isFound || !isMade {
			t.Fatalf("the held mint is %#v and the other %#v; want the other's key Made and the held one's Found", heldMint, second)
		}
		if found.Minted.Key.Principal() != made.Minted.Key.Principal() || found.Minted.Checkout == made.Minted.Checkout {
			t.Errorf("two worktrees minted %v and %v; want one principal and two checkout ids", found.Minted, made.Minted)
		}
		if !placed(found.Creates, files.Created, files.Existing) || !placed(made.Creates, files.Created, files.Created) {
			t.Errorf("the held mint made %+v and the other %+v; want the other's key created, the held one's found, and each its own checkout id", found.Creates, made.Creates)
		}
	})
	t.Run("a second mint returns the first identity", func(t *testing.T) {
		c := fresh(t)
		first, err := identityOf(mint(c.shared, c.worktree[0], 1))
		if err != nil {
			t.Fatal(err)
		}
		again, ok := mint(c.shared, c.worktree[0], 2).(Found)
		if !ok || again.Minted.Checkout != first.Checkout || again.Minted.Key.Principal() != first.Key.Principal() {
			t.Errorf("a second mint is %#v; want the first, %v, Found", again, first)
		}
		if !placed(again.Creates, files.Existing, files.Existing) {
			t.Errorf("a second mint made %+v; want both files existing", again.Creates)
		}
		if read := readIdentity(c.shared, c.worktree[0]); read != command.Identity(first) {
			t.Errorf("the files read as %#v; want the identity minted, %#v", read, first)
		}
	})
	t.Run("a damaged key is unreadable and an absent id unminted", func(t *testing.T) {
		c := fresh(t)
		if _, err := identityOf(mint(c.shared, c.worktree[0], 1)); err != nil {
			t.Fatal(err)
		}
		if got := readIdentity(c.shared, c.worktree[1]); got != (command.Unminted{Missing: command.Only{File: command.IDFile{}}}) {
			t.Errorf("a worktree with no id file reads as %#v; want its id file missing", got)
		}
		damaged := fresh(t)
		filestest.Create(t, damaged.shared, command.NameOf(command.PrincipalFile{}), []byte("ed25519 cut short\n"))
		if no, ok := mint(damaged.shared, damaged.worktree[0], 1).(NotMinted); !ok || !errors.Is(no.Why, errIdentityUnreadable) {
			t.Errorf("minting over a damaged key file = %#v; want NotMinted, errIdentityUnreadable", no)
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

func (h heldFiles) CreateOnce(p files.Name, b []byte) files.Placing {
	if p == command.NameOf(command.PrincipalFile{}) {
		close(h.reached)
		<-h.release
	}
	return h.Files.CreateOnce(p, b)
}

// identityDir is the directory a test's identity files are named in.
var identityDir = files.DirAt("/", files.MustName("identity"))

// onceIn is the identity file f in fs, named in identityDir.
func onceIn(fs files.Files, f command.File) onceFile {
	return onceFile{files: fs, dir: identityDir, name: command.NameOf(f)}
}

// mint is MintIdentity of the id in private and the key in shared, from
// entropy seeded by seed.
func mint(shared, private files.Files, seed byte) Minting {
	return MintIdentity(onceIn(private, command.IDFile{}), onceIn(shared, command.PrincipalFile{}), entropy(seed))
}

// identityOf is the identity m read back, or why there is none.
func identityOf(m Minting) (command.Minted, error) {
	switch m := m.(type) {
	case Found:
		return m.Minted, nil
	case Made:
		return m.Minted, nil
	case NotMinted:
		return command.Minted{}, m.Why
	case MadeUnread:
		return command.Minted{}, m.Why
	}
	panic(fmt.Sprintf("%T is not a Minting", m))
}

// placed is whether cs is the id file's durable create, made as id, then
// the key file's, made as key.
func placed(cs []command.Creation, id, key files.Made) bool {
	want := []struct {
		name string
		made files.Made
	}{{identityDir.PathOf(command.NameOf(command.IDFile{})), id}, {identityDir.PathOf(command.NameOf(command.PrincipalFile{})), key}}
	if len(cs) != len(want) {
		return false
	}
	for i, c := range cs {
		p, ok := c.Placing.(files.Placed)
		if !ok || c.Path != want[i].name || p.Made != want[i].made {
			return false
		}
	}
	return true
}

// The founding pin is no secret: on disk it is 0644, readable as the
// clone's other files are, unlike the identity files beside it.
func TestThePinOnDiskIsReadable(t *testing.T) {
	c := worktrees(t, t.TempDir())[1]
	if _, err := pin(&writes{stored: &command.StoreFacts{}}, onceFile{files: pinFiles(c), dir: pinDir(c), name: pinFile}, events.EventID{0x1}); err != nil {
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
