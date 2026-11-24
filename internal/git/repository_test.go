package git_test

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
)

func TestObjectsContract(t *testing.T) {
	t.Run("repository", func(t *testing.T) {
		gittest.Objects(t, func(t *testing.T) git.Objects {
			return gittest.Runner(t).RepositoryAt(filepath.Join(t.TempDir(), "repo"))
		})
	})
	t.Run("memory", func(t *testing.T) {
		gittest.Objects(t, func(*testing.T) git.Objects { return git.NewMemory() })
	})
}

func TestRefsContract(t *testing.T) {
	t.Run("repository", func(t *testing.T) {
		gittest.Refs(t, func(t *testing.T) gittest.Repository {
			return gittest.Runner(t).RepositoryAt(filepath.Join(t.TempDir(), "repo"))
		})
	})
	t.Run("memory", func(t *testing.T) {
		gittest.Refs(t, func(*testing.T) gittest.Repository { return git.NewMemory() })
	})
}

// TestTheNameGrammarIsASubsetOfGits holds the one name grammar to git:
// every name lit takes, git takes, as a refname and as a tree entry under
// fsck --strict; and every name the audit found lit taking where git
// refuses, git refuses.
func TestTheNameGrammarIsASubsetOfGits(t *testing.T) {
	taken := []string{"lit", "log", "0123456789abcdef", "A-b_9", "Z"}
	repo := gittest.Physical(t)
	gittest.Git(t, repo, "init", "-q", "--bare")
	blob := gittest.Git(t, repo, "hash-object", "-w", "--stdin")
	for _, n := range taken {
		if _, err := gittest.Ask(t, repo, "check-ref-format", "refs/lit/"+n); err != nil {
			t.Errorf("lit takes refs/lit/%s; git refuses it: %v", n, err)
		}
		tree := gittest.GitIn(t, repo, "100644 blob "+blob+"\t"+n+"\x00", "mktree", "-z")
		gittest.Git(t, repo, "update-ref", "refs/t", gittest.GitIn(t, repo, "m", "commit-tree", tree))
		if _, err := gittest.Ask(t, repo, "fsck", "--strict", "--no-dangling"); err != nil {
			t.Errorf("lit takes the tree entry %q; git fsck --strict refuses it: %v", n, err)
		}
	}
	for _, n := range gittest.Refused() {
		if strings.HasPrefix(string(n), "refs/") && !strings.ContainsRune(string(n), 0) {
			if _, err := gittest.Ask(t, repo, "check-ref-format", string(n)); err == nil {
				t.Errorf("lit refuses %q as the audit found git does; git check-ref-format takes it", n)
			}
		}
	}
}

// Locate finds what git rev-parse finds, from a main worktree, a linked
// one, a submodule and a directory nested in one.
func TestLocateFindsWhatGitFinds(t *testing.T) {
	root := gittest.Physical(t)
	main, linked, sub := filepath.Join(root, "main"), filepath.Join(root, "linked"), filepath.Join(root, "sub")
	gittest.Git(t, root, "init", "-q", "-b", "main", main)
	gittest.Git(t, main, "commit", "-q", "--allow-empty", "-m", "first")
	gittest.Git(t, main, "worktree", "add", "-q", linked)
	gittest.Git(t, root, "init", "-q", "-b", "main", sub)
	gittest.Git(t, sub, "commit", "-q", "--allow-empty", "-m", "first")
	gittest.Git(t, main, "-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "sub")
	nested := filepath.Join(main, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	g := gittest.Runner(t)
	for dir, want := range map[string]git.Checkout{
		main:                       {WorkTree: main, GitDir: filepath.Join(main, ".git"), CommonDir: filepath.Join(main, ".git"), Format: git.SHA1},
		nested:                     {WorkTree: main, GitDir: filepath.Join(main, ".git"), CommonDir: filepath.Join(main, ".git"), Format: git.SHA1},
		linked:                     {WorkTree: linked, GitDir: filepath.Join(main, ".git", "worktrees", "linked"), CommonDir: filepath.Join(main, ".git"), Format: git.SHA1},
		filepath.Join(main, "sub"): {WorkTree: filepath.Join(main, "sub"), GitDir: filepath.Join(main, ".git", "modules", "sub"), CommonDir: filepath.Join(main, ".git", "modules", "sub"), Format: git.SHA1},
	} {
		if got, err := g.Locate(dir); err != nil || got != want {
			t.Errorf("Locate(%s) = %+v, %v; want %+v", dir, got, err, want)
		}
	}
}

// Locate honors what git honors in the environment: GIT_DIR, as a hook
// runs with it, and GIT_CEILING_DIRECTORIES, which stops the walk.
func TestLocateReadsTheEnvironmentAsGitDoes(t *testing.T) {
	clone := gittest.Clone(t)
	elsewhere := gittest.Physical(t)
	c, err := gittest.Runner(t, "GIT_DIR="+clone.GitDir).Locate(elsewhere)
	if err != nil || c.GitDir != clone.GitDir || c.WorkTree != elsewhere {
		t.Errorf("Locate under GIT_DIR = %+v, %v; want its git dir with the working directory as its work tree", c, err)
	}
	nested := filepath.Join(clone.WorkTree, "a")
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = gittest.Runner(t, "GIT_CEILING_DIRECTORIES="+clone.WorkTree).Locate(nested)
	if want := (git.NotFound{Dir: nested}); err != want {
		t.Errorf("Locate below a ceiling = %v; want %v", err, want)
	}
}

func TestLocateWithNoGitIsNotFoundAndARefusalIsGits(t *testing.T) {
	dir := gittest.Physical(t)
	if _, err := gittest.Runner(t).Locate(dir); err != (git.NotFound{Dir: dir}) {
		t.Errorf("Locate(%s) failed with %v; want NotFound", dir, err)
	}
	bare := gittest.Physical(t)
	gittest.Git(t, bare, "init", "-q", "--bare")
	_, err := gittest.Runner(t).Locate(bare)
	if !errors.As(err, new(git.Refused)) || !strings.Contains(err.Error(), "work tree") {
		t.Errorf("Locate in a bare repository failed with %v; want git's refusal, which names the work tree", err)
	}
	// a pruned worktree: its .git names a git dir that is gone
	pruned := gittest.Physical(t)
	gone := filepath.Join(gittest.Physical(t), "worktrees", "gone")
	if err := os.WriteFile(filepath.Join(pruned, ".git"), []byte("gitdir: "+gone+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := gittest.Runner(t).Locate(pruned); !errors.As(err, new(git.Refused)) || !strings.Contains(err.Error(), gone) {
		t.Errorf("Locate in a pruned worktree failed with %v; want git's refusal, naming the git dir %s", err, gone)
	}
}

// lit's repository is created with its format and durability pinned: git
// fsyncs every object and ref it writes, and runs no gc or maintenance.
func TestLitsRepositoryIsCreatedPinned(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	if _, err := gittest.Put(gittest.Runner(t).RepositoryAt(dir), nil); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"core.fsync": "objects,reference", "core.fsyncMethod": "fsync", "gc.auto": "0", "maintenance.auto": "false", "extensions.objectFormat": "", "core.repositoryFormatVersion": "0"} {
		got, _ := gittest.Ask(t, dir, "config", "--file", "config", k)
		if strings.TrimSpace(got) != want {
			t.Errorf("lit's repository has %s = %q; want %q", k, strings.TrimSpace(got), want)
		}
	}
	if format := strings.TrimSpace(gittest.Git(t, dir, "rev-parse", "--show-ref-format")); format != "files" {
		t.Errorf("lit's repository keeps its refs as %s; want files", format)
	}
}

// A git missing from PATH is one loud error from every spawn, each on the
// record as never started.
func TestAGitMissingFromPathIsOneLoudError(t *testing.T) {
	g := git.Find([]string{"PATH=" + t.TempDir() + "::relative"}, func() time.Time { return gittest.Now })
	_, errLocate := g.Locate(t.TempDir())
	_, errPut := gittest.Put(g.RepositoryAt(filepath.Join(t.TempDir(), "repo")), nil)
	for _, err := range []error{errLocate, errPut} {
		if err == nil || !strings.Contains(err.Error(), "git is not in an absolute directory on PATH") {
			t.Errorf("a spawn with no git failed with %v; want the one error naming PATH", err)
		}
	}
	spawns := g.Spawns()
	if len(spawns) != 2 {
		t.Fatalf("with no git the record is %+v; want both attempts", spawns)
	}
	for _, s := range spawns {
		if u, ok := s.Ended.(git.Unstarted); !ok || !strings.Contains(u.Why, "PATH") {
			t.Errorf("an attempt with no git is recorded as %+v; want it never started, naming PATH", s)
		}
	}
}

// A Git that Find never made is a git missing from PATH, the same loud
// error, never a spawn of nothing.
func TestAZeroGitIsAMissingOne(t *testing.T) {
	g := &git.Git{}
	_, errLocate := g.Locate(t.TempDir())
	_, errPut := gittest.Put(g.RepositoryAt(filepath.Join(t.TempDir(), "repo")), nil)
	for _, err := range []error{errLocate, errPut} {
		if err == nil || !strings.Contains(err.Error(), "git is not in an absolute directory on PATH") {
			t.Errorf("a spawn of a zero Git failed with %v; want the one error naming PATH", err)
		}
	}
	if spawns := g.Spawns(); spawns == nil || len(spawns) != 2 {
		t.Errorf("a zero Git's record is %#v; want both attempts", spawns)
	}
}

// Every git process is on the record: its arguments, how it ended and its
// stderr. A read of lit's repository is go-git's, in process, and spawns
// nothing.
func TestEverySpawnIsRecorded(t *testing.T) {
	g := gittest.Runner(t)
	dir := gittest.Physical(t)
	if _, err := g.Locate(dir); err == nil {
		t.Fatal("Locate outside a repository succeeded")
	}
	refused := g.Spawns()
	if len(refused) != 1 || refused[0].Ended != (git.Exited{Code: 128}) || !strings.Contains(refused[0].Stderr, "not a git repository") || !slices.Contains(refused[0].Args, "rev-parse") {
		t.Errorf("the refused Locate is recorded as %+v; want rev-parse, exit 128 and git's stderr", refused)
	}
	repo := g.RepositoryAt(filepath.Join(dir, "repo"))
	id, err := gittest.Put(repo, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	written := g.Spawns()
	if last := written[len(written)-1]; last.Ended != (git.Exited{Code: 0}) || len(last.Args) == 0 {
		t.Errorf("the write is recorded as %+v; want its arguments and exit 0", last)
	}
	if _, err := repo.Blob(id); err != nil {
		t.Fatal(err)
	}
	if read := g.Spawns(); len(read) != len(written) {
		t.Errorf("a read spawned %+v", read[len(written):])
	}
}

func TestLocateReadsTheObjectFormatAsGitDoes(t *testing.T) {
	for _, f := range []git.ObjectFormat{git.SHA1, git.SHA256} {
		dir := gittest.Physical(t)
		gittest.Git(t, dir, "init", "-q", "--object-format="+f.String())
		if got, err := gittest.Runner(t).Locate(dir); err != nil || got.Format != f {
			t.Errorf("Locate in a %s repository = %+v, %v", f, got, err)
		}
	}
}

func TestRepositoryListsPackedAndLooseRefs(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	repo := gittest.Runner(t).RepositoryAt(dir)
	blob, err := gittest.Put(repo, []byte("hello\n"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := gittest.Put(repo, nil)
	if err != nil {
		t.Fatal(err)
	}
	gittest.Git(t, dir, "update-ref", "refs/lit/log/packed", blob.String())
	gittest.Git(t, dir, "update-ref", "refs/lit/log/both", blob.String())
	gittest.Git(t, dir, "pack-refs", "--all")
	if err := repo.Replace("refs/lit/log/both", git.Named{Target: blob}, other); err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace("refs/lit/log/loose", git.Absent{}, other); err != nil {
		t.Fatal(err)
	}
	l, err := repo.List("refs/lit/")
	want := []git.RefHead{{Name: "refs/lit/log/both", Target: other}, {Name: "refs/lit/log/loose", Target: other}, {Name: "refs/lit/log/packed", Target: blob}}
	if err != nil || !slices.Equal(l.Heads, want) || len(l.Broken) != 0 {
		t.Errorf("List = %+v, %v; want %v, the loose ref over its packed copy", l, err, want)
	}
}

// An object of a kind lit never writes, a tag, is NotLit, the refusal of
// every object lit did not write, never a failure of another kind.
func TestATagIsNotLit(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	repo := gittest.Runner(t).RepositoryAt(dir)
	blob, err := gittest.Put(repo, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	tag := gittest.ID(t, gittest.GitIn(t, dir, "object "+blob.String()+"\ntype blob\ntag t\ntagger t <t@t> 0 +0000\n\nt\n", "mktag"))
	if _, err := repo.Commit(tag); !errors.As(err, new(git.NotLit)) {
		t.Errorf("reading a tag as a commit failed with %v; want NotLit", err)
	}
}

// A ref go-git cannot read as naming an object, which git never writes, is
// listed as broken by name beside every other ref, and the listing, a
// read, leaves it there; an empty HEAD, which no listing reads, is left
// too. Its writer's replace from Broken repairs it.
func TestABrokenRefIsListedAndItsWritersReplaceRepairsIt(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	repo := gittest.Runner(t).RepositoryAt(dir)
	blob, err := gittest.Put(repo, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace("refs/lit/log/good", git.Absent{}, blob); err != nil {
		t.Fatal(err)
	}
	torn := filepath.Join(dir, "refs", "lit", "log", "torn")
	for _, text := range []string{"", "torn", "ref: refs/lit/log/good", strings.Repeat("0", 40) + "\n"} {
		if err := os.WriteFile(torn, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		l, err := repo.List("refs/lit/")
		if err != nil || !slices.Equal(l.Heads, []git.RefHead{{Name: "refs/lit/log/good", Target: blob}}) || len(l.Broken) != 1 || l.Broken[0].Name != "refs/lit/log/torn" {
			t.Fatalf("with a ref file holding %q, List = %+v, %v; want refs/lit/log/good, and refs/lit/log/torn broken", text, l, err)
		}
		if got, err := os.ReadFile(torn); err != nil || string(got) != text {
			t.Errorf("listing changed the broken ref holding %q: %q, %v", text, got, err)
		}
		if err := repo.Replace("refs/lit/log/torn", git.Broken{Ref: l.Broken[0]}, blob); err != nil {
			t.Errorf("replacing the ref holding %q from Broken failed: %v", text, err)
		}
		if l, err := repo.List("refs/lit/log/torn"); err != nil || !slices.Equal(l.Heads, []git.RefHead{{Name: "refs/lit/log/torn", Target: blob}}) {
			t.Errorf("after the repair the ref is %+v, %v; want it naming %s", l, err, blob)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if l, err := repo.List("refs/lit/"); err != nil || len(l.Heads) != 2 {
		t.Errorf("with an empty HEAD, List = %+v, %v; want both refs", l, err)
	}
	if info, err := os.Stat(filepath.Join(dir, "HEAD")); err != nil || info.Size() != 0 {
		t.Errorf("listing touched the empty HEAD: %v, %v", info, err)
	}
}

// A ref made whole by another writer after it was read broken is kept: the
// repair reads it again under git's lock, and the replace from Broken is
// then refused as the race it is.
func TestARepairKeepsARefMadeWholeSinceItWasRead(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	repo := gittest.Runner(t).RepositoryAt(dir)
	blob, err := gittest.Put(repo, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	other, err := gittest.Put(repo, []byte("y"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace("refs/lit/log/w", git.Absent{}, blob); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "refs", "lit", "log", "w")
	if err := os.WriteFile(path, []byte("torn"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := repo.List("refs/lit/")
	if err != nil || len(l.Broken) != 1 {
		t.Fatalf("List = %+v, %v; want refs/lit/log/w broken", l, err)
	}
	// the other writer's repair lands between this one's read and its own
	if err := os.WriteFile(path, []byte(other.String()+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace("refs/lit/log/w", git.Broken{Ref: l.Broken[0]}, blob); err == nil {
		t.Error("a replace from Broken over a ref made whole since = nil; want it refused")
	}
	if l, err := repo.List("refs/lit/"); err != nil || !slices.Equal(l.Heads, []git.RefHead{{Name: "refs/lit/log/w", Target: other}}) {
		t.Errorf("after the refused repair the ref is %+v, %v; want the other writer's %s", l, err, other)
	}
}

// A lock a killed git left on a ref is cleared by the next replace, so no
// write of the ref is refused for good; a lock young enough to be a write
// under way is left, and the replace refused as a race.
func TestAStaleLockIsClearedAndALiveOneKept(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	repo := gittest.Runner(t).RepositoryAt(dir)
	blob, err := gittest.Put(repo, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "refs", "lit", "log"), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(dir, "refs", "lit", "log", "w.lock")
	aged := func(age time.Duration) {
		t.Helper()
		at := gittest.Now.Add(-age)
		if err := os.WriteFile(lock, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(lock, at, at); err != nil {
			t.Fatal(err)
		}
	}
	aged(9 * time.Minute)
	if err := repo.Replace("refs/lit/log/w", git.Absent{}, blob); err == nil {
		t.Error("a replace under a live lock = nil; want it refused")
	}
	aged(11 * time.Minute)
	if err := repo.Replace("refs/lit/log/w", git.Absent{}, blob); err != nil {
		t.Errorf("a replace under a stale lock failed: %v", err)
	}
}

// A write is one git process for each kind it holds, however many objects
// that is, and the version check before it runs once per Git.
func TestAWriteIsOneSpawnPerKind(t *testing.T) {
	g := gittest.Runner(t)
	repo := g.RepositoryAt(filepath.Join(gittest.Physical(t), "repo"))
	if _, err := gittest.Put(repo, nil); err != nil {
		t.Fatal(err)
	}
	before := len(g.Spawns())
	bs := make([][]byte, 100)
	for i := range bs {
		bs[i] = fmt.Appendf(nil, "blob %d", i)
	}
	if _, err := repo.PutBlobs(bs); err != nil {
		t.Fatal(err)
	}
	spawns := g.Spawns()
	if len(spawns) != before+1 {
		t.Errorf("putting %d blobs spawned %+v; want one git process", len(bs), spawns[before:])
	}
	versions := 0
	for _, s := range spawns {
		if slices.Contains(s.Args, "version") {
			versions++
		}
	}
	if versions != 1 {
		t.Errorf("two writes asked git its version %d times; want once", versions)
	}
}

// A GIT_ variable of the caller's, as a hook sets one, never sends lit's
// objects anywhere but lit's repository.
func TestTheCallersGitVariablesNeverRedirectAWrite(t *testing.T) {
	root := gittest.Physical(t)
	elsewhere := filepath.Join(root, "elsewhere")
	gittest.Git(t, root, "init", "-q", "--bare", elsewhere)
	dir := filepath.Join(root, "repo")
	g := gittest.Runner(t, "GIT_DIR="+elsewhere, "GIT_OBJECT_DIRECTORY="+filepath.Join(elsewhere, "objects"), "GIT_INDEX_FILE="+filepath.Join(elsewhere, "index"))
	id, err := gittest.Put(g.RepositoryAt(dir), []byte("mine"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gittest.Ask(t, dir, "cat-file", "-e", id.String()); err != nil {
		t.Errorf("the blob is not in lit's repository: %v", err)
	}
	if _, err := gittest.Ask(t, elsewhere, "cat-file", "-e", id.String()); err == nil {
		t.Errorf("the blob was written into the repository the caller's GIT_ variables name")
	}
}

// git writes a ref as its lock file, renamed into place: a lock, empty or
// half-written, is never a ref, so a listing during a write sees the ref
// as it was; nor is a dotfile, a name git never reads as a ref.
func TestALockFileIsNoRef(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	repo := gittest.Runner(t).RepositoryAt(dir)
	blob, err := gittest.Put(repo, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace("refs/lit/log/a", git.Absent{}, blob); err != nil {
		t.Fatal(err)
	}
	for _, lock := range []string{"", blob.String() + "\n"} {
		for _, n := range []string{"a.lock", "b.lock", ".b", ".tmp-c"} {
			if err := os.WriteFile(filepath.Join(dir, "refs", "lit", "log", n), []byte(lock), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if l, err := repo.List("refs/lit/"); err != nil || !slices.Equal(l.Heads, []git.RefHead{{Name: "refs/lit/log/a", Target: blob}}) || len(l.Broken) != 0 {
			t.Errorf("with lock files and dotfiles holding %q, List = %+v, %v; want refs/lit/log/a alone", lock, l, err)
		}
	}
}

func TestReadingCreatesNoRepository(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	g := gittest.Runner(t)
	repo := g.RepositoryAt(dir)
	if _, err := repo.Blob(gittest.ID(t, "ce013625030ba8dba906f756967f9e9ca394464a")); !errors.As(err, new(git.MissingObject)) {
		t.Errorf("reading an absent repository failed with %v; want MissingObject", err)
	}
	if l, err := repo.List("refs/lit/"); err != nil || len(l.Heads)+len(l.Broken) != 0 {
		t.Errorf("listing an absent repository = %+v, %v; want nothing", l, err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("reading created %s: %v", dir, err)
	}
	if spawns := g.Spawns(); len(spawns) != 0 {
		t.Errorf("reading an absent repository ran %v", spawns)
	}
}

// A directory at the path that is not a repository is refused, read or
// written, never taken as an empty one.
func TestADirectoryThatIsNoRepositoryIsRefused(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := gittest.Runner(t).RepositoryAt(dir)
	_, errPut := gittest.Put(repo, []byte("x"))
	_, errList := repo.List("refs/lit/")
	_, errRead := repo.Blob(gittest.ID(t, "ce013625030ba8dba906f756967f9e9ca394464a"))
	for _, err := range []error{errPut, errList, errRead} {
		if err == nil || !strings.Contains(err.Error(), dir+" is no git repository") {
			t.Errorf("using a directory that is no repository failed with %v; want its refusal, naming it", err)
		}
	}
}

// A tree or commit git holds that lit did not write is refused, never read
// as less than it is: a commit with another identity, a time or a parent,
// a tree with an executable entry.
func TestAnObjectLitDidNotWriteIsRefused(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	repo := gittest.Runner(t).RepositoryAt(dir)
	blob, err := gittest.Put(repo, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	tree, _, err := repo.PutTree([]git.Entry{{Name: "a", Blob: blob}})
	if err != nil {
		t.Fatal(err)
	}
	lit, _, err := repo.PutCommit(git.Commit{Tree: tree, Message: "m\n"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gittest.Ask(t, dir, "fsck", "--strict"); err != nil {
		t.Errorf("git fsck --strict refuses what lit wrote: %v", err)
	}
	foreign := gittest.GitIn(t, dir, "m\n", "commit-tree", tree.String())
	child := gittest.GitIn(t, dir, "m\n", "commit-tree", "-p", lit.String(), tree.String())
	for _, c := range []string{foreign, child} {
		if got, err := repo.Commit(gittest.ID(t, c)); err == nil {
			t.Errorf("Commit(%s) read a commit lit did not write as %+v", c, got)
		}
	}
	exec := gittest.GitIn(t, dir, "100755 blob "+blob.String()+"\ta\x00", "mktree", "-z")
	twice := gittest.GitIn(t, dir, "100644 blob "+blob.String()+"\ta\x00100644 blob "+blob.String()+"\ta\x00", "mktree", "-z")
	for _, tr := range []string{exec, twice} {
		if got, err := repo.Tree(gittest.ID(t, tr)); err == nil {
			t.Errorf("Tree(%s) read a tree lit did not write as %v", tr, got)
		}
	}
}

func TestTwoIDsOfOneSumInTwoFormatsDiffer(t *testing.T) {
	const sum = "ce013625030ba8dba906f756967f9e9ca394464a"
	sha1, err := git.ParseObjectID(git.SHA1, sum)
	if err != nil {
		t.Fatal(err)
	}
	sha256, err := git.ParseObjectID(git.SHA256, sum+"000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	short, err := git.ParseObjectID(git.SHA256, sum)
	if sha1 == sha256 || err == nil || short == sha1 {
		t.Errorf("a sha1 id and a sha256 id compare equal, or a sha1 sum reads as a sha256 id")
	}
	if got := hex.EncodeToString(sha1.Sum()); got != sum {
		t.Errorf("a sha1 id's sum is %s; want %s", got, sum)
	}
	if got := hex.EncodeToString(sha256.Sum()); got != sum+"000000000000000000000000" {
		t.Errorf("a sha256 id's sum is %s; want its 32 bytes", got)
	}
	for _, id := range []git.ObjectID{sha1, sha256} {
		if back, err := git.ObjectIDFrom(id.Format(), id.Sum()); err != nil || back != id {
			t.Errorf("ObjectIDFrom(%v, Sum) = %v, %v; want %v", id.Format(), back, err, id)
		}
	}
	for name, read := range map[string]func() (git.ObjectID, error){
		"a sha1 sum as sha256": func() (git.ObjectID, error) { return git.ObjectIDFrom(git.SHA256, sha1.Sum()) },
		"a sha256 sum as sha1": func() (git.ObjectID, error) { return git.ObjectIDFrom(git.SHA1, sha256.Sum()) },
		"no format":            func() (git.ObjectID, error) { return git.ObjectIDFrom(0, sha1.Sum()) },
		"an empty sum":         func() (git.ObjectID, error) { return git.ObjectIDFrom(git.SHA1, nil) },
	} {
		if id, err := read(); err == nil {
			t.Errorf("%s reads as %v; want it refused", name, id)
		}
	}
}

// The zero id names no object: it prints as such, and no format reads one.
func TestTheZeroIDPrintsAsNoIDAndNoFormatReadsOne(t *testing.T) {
	if got := fmt.Sprintf("%v %+v", git.ObjectID{}, git.ObjectID{}.Format()); got != "(no object id) no format (0)" {
		t.Errorf("the zero id prints as %q", got)
	}
	if _, err := git.ParseObjectID(0, "ce013625030ba8dba906f756967f9e9ca394464a"); err == nil {
		t.Errorf("an id read in no format was taken")
	}
}
