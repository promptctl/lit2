package root

import (
	"errors"
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
	if _, err := MintIdentity(shared, private, entropy(1), func(events.Key) {}); err != nil {
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
		var made [2][]command.Creation
		var fresh [2][]events.Key
		held := heldAtKeyCreate(creates(c.shared, &made[0]))
		var got [2]command.Minted
		first := make(chan error, 1)
		go func() {
			var err error
			got[0], err = MintIdentity(held, creates(c.worktree[0], &made[0]), entropy(0), keep(&fresh[0]))
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
		if got[1], err = MintIdentity(creates(c.shared, &made[1]), creates(c.worktree[1], &made[1]), entropy(1), keep(&fresh[1])); err != nil {
			t.Fatalf("the second mint: %v", err)
		}
		close(held.release)
		if err := <-first; err != nil {
			t.Fatalf("the first mint: %v", err)
		}
		if got[0].Key.Principal() != got[1].Key.Principal() || got[0].Checkout == got[1].Checkout {
			t.Errorf("two worktrees minted %v and %v; want one principal and two checkout ids", got[0], got[1])
		}
		if !placed(made[0], files.Created, files.Existing) || !placed(made[1], files.Created, files.Created) {
			t.Errorf("the held mint made %+v and the other %+v; want the other's key created, the held one's found, and each its own checkout id", made[0], made[1])
		}
		if len(fresh[0]) != 0 || len(fresh[1]) != 1 || fresh[1][0].Principal() != got[1].Key.Principal() {
			t.Errorf("the held mint had keys %v made and the other %v; want only the other's, the one both sign with", fresh[0], fresh[1])
		}
	})
	t.Run("a second mint returns the first identity", func(t *testing.T) {
		c := fresh(t)
		first, err := MintIdentity(c.shared, c.worktree[0], entropy(1), func(events.Key) {})
		if err != nil {
			t.Fatal(err)
		}
		var made []command.Creation
		var keys []events.Key
		again, err := MintIdentity(creates(c.shared, &made), creates(c.worktree[0], &made), entropy(2), keep(&keys))
		if err != nil || again.Checkout != first.Checkout || again.Key.Principal() != first.Key.Principal() {
			t.Errorf("a second mint is %v, %v; want the first, %v", again, err, first)
		}
		if !placed(made, files.Existing, files.Existing) || len(keys) != 0 {
			t.Errorf("a second mint made %+v and had keys %v made; want both files existing, and no key made", made, keys)
		}
		if read := readIdentity(c.shared, c.worktree[0]); read != command.Identity(first) {
			t.Errorf("the files read as %#v; want the identity minted, %#v", read, first)
		}
	})
	t.Run("a damaged key is unreadable and an absent id unminted", func(t *testing.T) {
		c := fresh(t)
		if _, err := MintIdentity(c.shared, c.worktree[0], entropy(1), func(events.Key) {}); err != nil {
			t.Fatal(err)
		}
		if got := readIdentity(c.shared, c.worktree[1]); got != (command.Unminted{Missing: command.Only{File: command.IDFile{}}}) {
			t.Errorf("a worktree with no id file reads as %#v; want its id file missing", got)
		}
		damaged := fresh(t)
		filestest.Create(t, damaged.shared, command.NameOf(command.PrincipalFile{}), []byte("ed25519 cut short\n"))
		if _, err := MintIdentity(damaged.shared, damaged.worktree[0], entropy(1), func(events.Key) {}); !errors.Is(err, errIdentityUnreadable) {
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

func (h heldFiles) CreateOnce(p files.Name, b []byte) files.Placing {
	if p == command.NameOf(command.PrincipalFile{}) {
		close(h.reached)
		<-h.release
	}
	return h.Files.CreateOnce(p, b)
}

// creates is fs with each create kept in into, its file named by its name.
func creates(fs files.Files, into *[]command.Creation) files.Files {
	return recorded{Files: fs, at: files.Name.String, record: func(c command.Creation) { *into = append(*into, c) }}
}

// keep is a mint's made that keeps each key made in into.
func keep(into *[]events.Key) func(events.Key) {
	return func(k events.Key) { *into = append(*into, k) }
}

// placed is whether cs is the id file's durable create, made as id, then
// the key file's, made as key.
func placed(cs []command.Creation, id, key files.Made) bool {
	want := []struct {
		name string
		made files.Made
	}{{command.NameOf(command.IDFile{}).String(), id}, {command.NameOf(command.PrincipalFile{}).String(), key}}
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
	if _, err := pin(pinFiles(c), events.EventID{0x1}); err != nil {
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
