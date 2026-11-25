package git_test

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
)

// repositoryAt is lit's repository at path, it and the directory it is in
// checked from the directory above.
func repositoryAt(g *git.Git, path string) git.Repository {
	in := filepath.Dir(path)
	return g.RepositoryAt(files.DirAt(filepath.Dir(in), files.MustName(filepath.Base(in))).Sub(files.MustName(filepath.Base(path))))
}

func TestObjectsContract(t *testing.T) {
	t.Run("repository", func(t *testing.T) {
		gittest.Objects(t, func(t *testing.T) git.Objects {
			return repositoryAt(gittest.Runner(t), filepath.Join(t.TempDir(), "repo"))
		})
	})
	t.Run("memory", func(t *testing.T) {
		gittest.Objects(t, func(*testing.T) git.Objects { return git.NewMemory() })
	})
}

func TestRefsContract(t *testing.T) {
	t.Run("repository", func(t *testing.T) {
		gittest.Refs(t, func(t *testing.T) gittest.Repository {
			return repositoryAt(gittest.Runner(t), filepath.Join(t.TempDir(), "repo"))
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
			// check-ref-format refuses a name with status 1; the names taken
			// above are its control
			if code := gittest.Exit(t, repo, "check-ref-format", string(n)); code != 1 {
				t.Errorf("lit refuses %q as the audit found git does; git check-ref-format exits %d on it, not 1", n, code)
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
	// refusedBy is whether err is git's refusal of Locate's rev-parse in
	// dir, exiting as git itself exits there
	refusedBy := func(t *testing.T, dir string, err error) bool {
		t.Helper()
		var refused git.Refused
		if !errors.As(err, &refused) || len(refused.Spawn.Args) == 0 || refused.Spawn.Args[0] != "rev-parse" {
			return false
		}
		return refused.Spawn.Ended == git.Ended(git.Exited{Code: gittest.Exit(t, dir, "rev-parse", "--show-toplevel")})
	}
	// [LAW:verifiable-goals] exception: git's refusal, with no identity lit can read; proven by its minimal pair
	root := gittest.Physical(t)
	worktree, bare := filepath.Join(root, "worktree"), filepath.Join(root, "bare")
	gittest.Git(t, root, "init", "-q", worktree)
	gittest.Git(t, root, "init", "-q", "--bare", bare)
	if _, err := gittest.Runner(t).Locate(worktree); err != nil {
		t.Fatalf("control: Locate in a repository with a work tree failed with %v", err)
	}
	if _, err := gittest.Runner(t).Locate(bare); !refusedBy(t, bare, err) {
		t.Errorf("Locate in a bare repository failed with %v; want git's refusal of its rev-parse", err)
	}
	// [LAW:verifiable-goals] exception: git's refusal, with no identity lit can read; proven by its minimal pair
	main, linked := filepath.Join(root, "main"), filepath.Join(root, "linked")
	gittest.Git(t, root, "init", "-q", "-b", "main", main)
	gittest.Git(t, main, "commit", "-q", "--allow-empty", "-m", "first")
	gittest.Git(t, main, "worktree", "add", "-q", linked)
	if _, err := gittest.Runner(t).Locate(linked); err != nil {
		t.Fatalf("control: Locate in a live linked worktree failed with %v", err)
	}
	// pruned: its .git names a git dir that is gone
	gone := filepath.Join(main, ".git", "worktrees", "linked")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	if _, err := gittest.Runner(t).Locate(linked); !refusedBy(t, linked, err) || !strings.Contains(err.Error(), gone) {
		t.Errorf("Locate in a pruned worktree failed with %v; want git's refusal of its rev-parse, naming the git dir %s", err, gone)
	}
}

// lit's repository is created with its format and durability pinned: git
// fsyncs every object and ref it writes, and runs no gc or maintenance.
func TestLitsRepositoryIsCreatedPinned(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	if _, err := gittest.Put(repositoryAt(gittest.Runner(t), dir), nil); err != nil {
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
	g := git.Find([]string{"PATH=" + t.TempDir() + "::relative"})
	_, errLocate := g.Locate(t.TempDir())
	_, errPut := gittest.Put(repositoryAt(g, filepath.Join(t.TempDir(), "repo")), nil)
	for _, err := range []error{errLocate, errPut} {
		if !errors.Is(err, git.ErrNoGit) {
			t.Errorf("a spawn with no git failed with %v; want ErrNoGit", err)
		} else if !strings.Contains(err.Error(), "absolute directory on PATH") {
			t.Errorf("a spawn with no git failed with %v; want it to say where git is looked for", err)
		}
	}
	spawns := g.Spawns()
	if len(spawns) != 2 {
		t.Fatalf("with no git the record is %+v; want both attempts", spawns)
	}
	for _, s := range spawns {
		if unstarted, ok := s.Ended.(git.Unstarted); !ok || !errors.Is(unstarted.Why, git.ErrNoGit) {
			t.Errorf("an attempt with no git is recorded as %+v; want it never started, for ErrNoGit", s)
		}
	}
	putsWithGit(t)
}

// A Git that Find never made is a git missing from PATH, the same loud
// error, never a spawn of nothing.
func TestAZeroGitIsAMissingOne(t *testing.T) {
	g := &git.Git{}
	_, errLocate := g.Locate(t.TempDir())
	_, errPut := gittest.Put(repositoryAt(g, filepath.Join(t.TempDir(), "repo")), nil)
	for _, err := range []error{errLocate, errPut} {
		if !errors.Is(err, git.ErrNoGit) {
			t.Errorf("a spawn of a zero Git failed with %v; want ErrNoGit", err)
		}
	}
	if spawns := g.Spawns(); spawns == nil || len(spawns) != 2 {
		t.Errorf("a zero Git's record is %#v; want both attempts", spawns)
	}
	putsWithGit(t)
}

// putsWithGit is the control of a git missing: the same put, by a git Find
// finds on PATH, is stored.
func putsWithGit(t *testing.T) {
	t.Helper()
	if _, err := gittest.Put(repositoryAt(gittest.Runner(t), filepath.Join(t.TempDir(), "repo")), nil); err != nil {
		t.Errorf("a put by a git on PATH failed with %v", err)
	}
}

// Every git process is on the record: its arguments, how it ended and its
// stderr. A read of lit's repository is go-git's, in process, and spawns
// nothing.
func TestEverySpawnIsRecorded(t *testing.T) {
	g := gittest.Runner(t)
	dir := gittest.Physical(t)
	if _, err := g.Locate(dir); err != (git.NotFound{Dir: dir}) {
		t.Fatalf("Locate outside a repository = %v; want NotFound", err)
	}
	refused := g.Spawns()
	if len(refused) != 1 || refused[0].Ended != (git.Exited{Code: 128}) || !strings.Contains(refused[0].Stderr, "not a git repository") || !slices.Contains(refused[0].Args, "rev-parse") {
		t.Errorf("the refused Locate is recorded as %+v; want rev-parse, exit 128 and git's stderr", refused)
	}
	repo := repositoryAt(g, filepath.Join(dir, "repo"))
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
	repo := repositoryAt(gittest.Runner(t), dir)
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
	repo := repositoryAt(gittest.Runner(t), dir)
	blob, err := gittest.Put(repo, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	tag := gittest.ID(t, gittest.GitIn(t, dir, "object "+blob.String()+"\ntype blob\ntag t\ntagger t <t@t> 0 +0000\n\nt\n", "mktag"))
	if _, err := repo.Commit(tag); !errors.As(err, new(git.NotLit)) || !errors.Is(err, git.ErrWrongKind) {
		t.Errorf("reading a tag as a commit failed with %v; want NotLit, ErrWrongKind", err)
	}
	// the control: a commit lit wrote, in the same repository, reads
	commit := gittest.PutCommit(t, repo, git.Commit{Tree: gittest.PutTree(t, repo, nil), Message: "m\n"})
	if _, err := repo.Commit(commit); err != nil {
		t.Errorf("reading a commit lit wrote failed with %v", err)
	}
}

// A ref go-git cannot read as naming an object, which git never writes, is
// listed as broken by name beside every other ref, and the listing, a
// read, leaves it there; an empty HEAD, which no listing reads, is left
// too.
func TestABrokenRefIsListedAndLeftByARead(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	repo := repositoryAt(gittest.Runner(t), dir)
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
			t.Errorf("with a ref file holding %q, List = %+v, %v; want refs/lit/log/good, and refs/lit/log/torn broken", text, l, err)
		} else if !strings.Contains(l.Broken[0].Why, strings.TrimSpace(text)) {
			t.Errorf("a ref file holding %q is broken because %q; want what it holds said", text, l.Broken[0].Why)
		}
		if got, err := os.ReadFile(torn); err != nil || string(got) != text {
			t.Errorf("listing changed the broken ref holding %q: %q, %v", text, got, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if l, err := repo.List("refs/lit/"); err != nil || len(l.Heads) != 1 || len(l.Broken) != 1 {
		t.Errorf("with an empty HEAD, List = %+v, %v; want both refs as before", l, err)
	}
	if info, err := os.Stat(filepath.Join(dir, "HEAD")); err != nil || info.Size() != 0 {
		t.Errorf("listing touched the empty HEAD: %v, %v", info, err)
	}
}

// Objects a pack brings after the storage was opened, as a fetch or a
// repack brings them, are read, with no other step between.
func TestAPackArrivingAfterTheFirstReadIsRead(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	repo := repositoryAt(gittest.Runner(t), dir)
	first, err := gittest.Put(repo, []byte("first"))
	if err != nil {
		t.Fatal(err)
	}
	// the storage is opened by a read of another object, so the blob is in
	// no cache when it is read after the repack
	if _, err := repo.Blob(first); err != nil {
		t.Fatal(err)
	}
	blob, err := gittest.Put(repo, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	// a ref makes the blob reachable, so the repack moves it out of its loose file
	if err := repo.Replace("refs/lit/log/w", git.Absent{}, blob); err != nil {
		t.Fatal(err)
	}
	gittest.Git(t, dir, "repack", "-a", "-d", "-q")
	if loose, _ := filepath.Glob(filepath.Join(dir, "objects", blob.String()[:2], "*")); len(loose) != 0 {
		t.Fatalf("the repack left the blob loose: %v", loose)
	}
	if _, err := repo.Blob(blob); err != nil {
		t.Errorf("a blob moved into a new pack read as %v; want it read", err)
	}
}

// A write is one git process for each kind it holds, however many objects
// that is, and the version check before it runs once per Git.
func TestAWriteIsOneSpawnPerKind(t *testing.T) {
	g := gittest.Runner(t)
	repo := repositoryAt(g, filepath.Join(gittest.Physical(t), "repo"))
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
	id, err := gittest.Put(repositoryAt(g, dir), []byte("mine"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gittest.Ask(t, dir, "cat-file", "-e", id.String()); err != nil {
		t.Errorf("the blob is not in lit's repository: %v", err)
	}
	// cat-file -e says an object is absent with status 1; in lit's
	// repository above, the same question is the control
	if code := gittest.Exit(t, elsewhere, "cat-file", "-e", id.String()); code != 1 {
		t.Errorf("cat-file -e of the blob in the repository the caller's GIT_ variables name exits %d; want 1, absent", code)
	}
}

// git writes a ref as its lock file, renamed into place: a lock, empty or
// half-written, is never a ref, so a listing during a write sees the ref
// as it was; nor is a dotfile, a name git never reads as a ref.
func TestALockFileIsNoRef(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	repo := repositoryAt(gittest.Runner(t), dir)
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

// A replace git refuses is Moved only when the ref, read after the
// refusal, has moved: a ref git cannot lock, its directory closed to
// writes, is still what its writer read, so the refusal is git's own,
// Refused; a ref that will not read after the refusal says that too,
// beside git's.
func TestAReplaceGitRefusesIsSaidByTheRefsStateAfter(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	repo := repositoryAt(gittest.Runner(t), dir)
	blob, err := gittest.Put(repo, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace("refs/lit/log/control", git.Absent{}, blob); err != nil {
		t.Fatalf("control: a replace from absent failed with %v", err)
	}
	logs := filepath.Join(dir, "refs", "lit", "log")
	if err := os.Chmod(logs, 0o555); err != nil {
		t.Fatal(err)
	}
	err = repo.Replace("refs/lit/log/closed", git.Absent{}, blob)
	if err := os.Chmod(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	if !errors.As(err, new(git.Refused)) || errors.As(err, new(git.Moved)) {
		t.Errorf("a replace of a ref git cannot lock, still absent, = %v; want Refused, not Moved", err)
	}
	if err := os.WriteFile(filepath.Join(logs, "torn"), []byte("torn"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = repo.Replace("refs/lit/log/torn", git.Absent{}, blob)
	var broken git.BrokenRef
	if !errors.As(err, new(git.Refused)) || !errors.As(err, &broken) || broken.Name != "refs/lit/log/torn" {
		t.Errorf("a replace of a ref that will not read = %v; want Refused and the ref broken", err)
	}
}

func TestReadingCreatesNoRepository(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	g := gittest.Runner(t)
	repo := repositoryAt(g, dir)
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
	repo := repositoryAt(gittest.Runner(t), dir)
	_, errPut := gittest.Put(repo, []byte("x"))
	_, errList := repo.List("refs/lit/")
	_, errRead := repo.Blob(gittest.ID(t, "ce013625030ba8dba906f756967f9e9ca394464a"))
	for _, err := range []error{errPut, errList, errRead} {
		if !errors.Is(err, git.ErrNoRepository) || !strings.Contains(err.Error(), dir) {
			t.Errorf("using a directory that is no repository failed with %v; want ErrNoRepository, naming it", err)
		}
	}
	// the control: the same put where no directory stands is stored
	if _, err := gittest.Put(repositoryAt(gittest.Runner(t), filepath.Join(filepath.Dir(dir), "fresh")), []byte("x")); err != nil {
		t.Errorf("a put where no directory stands failed with %v", err)
	}
}

// A tree or commit git holds that lit did not write is refused, never read
// as less than it is: a commit with another identity, a time or a parent,
// a tree with an executable entry.
func TestAnObjectLitDidNotWriteIsRefused(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	repo := repositoryAt(gittest.Runner(t), dir)
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
	// the controls: the commit and tree lit wrote read, so each refusal
	// below is its object's shape
	if _, err := repo.Commit(lit); err != nil {
		t.Errorf("Commit(%s), lit's own, failed with %v", lit, err)
	}
	if _, err := repo.Tree(tree); err != nil {
		t.Errorf("Tree(%s), lit's own, failed with %v", tree, err)
	}
	for _, c := range []string{foreign, child} {
		if got, err := repo.Commit(gittest.ID(t, c)); !errors.Is(err, git.ErrForeignShape) || !errors.As(err, new(git.NotLit)) {
			t.Errorf("Commit(%s) = %+v, %v; want NotLit, ErrForeignShape", c, got, err)
		}
	}
	exec := gittest.GitIn(t, dir, "100755 blob "+blob.String()+"\ta\x00", "mktree", "-z")
	twice := gittest.GitIn(t, dir, "100644 blob "+blob.String()+"\ta\x00100644 blob "+blob.String()+"\ta\x00", "mktree", "-z")
	for _, tr := range []string{exec, twice} {
		if got, err := repo.Tree(gittest.ID(t, tr)); !errors.Is(err, git.ErrForeignShape) || !errors.As(err, new(git.NotLit)) {
			t.Errorf("Tree(%s) = %v, %v; want NotLit, ErrForeignShape", tr, got, err)
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
	if sha1 == sha256 {
		t.Errorf("a sha1 id and a sha256 id of one sum compare equal")
	}
	if short, err := git.ParseObjectID(git.SHA256, sum); !errors.Is(err, git.ErrSumSize) {
		t.Errorf("a sha1 sum read as a sha256 id = %v, %v; want ErrSumSize", short, err)
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
	// the round trips above are the control: each sum read in its own
	// format is an id
	for name, c := range map[string]struct {
		read func() (git.ObjectID, error)
		want error
	}{
		"a sha1 sum as sha256": {func() (git.ObjectID, error) { return git.ObjectIDFrom(git.SHA256, sha1.Sum()) }, git.ErrSumSize},
		"a sha256 sum as sha1": {func() (git.ObjectID, error) { return git.ObjectIDFrom(git.SHA1, sha256.Sum()) }, git.ErrSumSize},
		"no format":            {func() (git.ObjectID, error) { return git.ObjectIDFrom(0, sha1.Sum()) }, git.ErrNoFormat},
		"an empty sum":         {func() (git.ObjectID, error) { return git.ObjectIDFrom(git.SHA1, nil) }, git.ErrSumSize},
	} {
		if id, err := c.read(); !errors.Is(err, c.want) {
			t.Errorf("%s reads as %v, %v; want %v", name, id, err, c.want)
		}
	}
}

// The zero id names no object: it prints as such, and no format reads one.
func TestTheZeroIDPrintsAsNoIDAndNoFormatReadsOne(t *testing.T) {
	if got := fmt.Sprintf("%v %+v", git.ObjectID{}, git.ObjectID{}.Format()); got != "(no object id) no format (0)" {
		t.Errorf("the zero id prints as %q", got)
	}
	const sum = "ce013625030ba8dba906f756967f9e9ca394464a"
	if _, err := git.ParseObjectID(git.SHA1, sum); err != nil {
		t.Errorf("the id read in its format failed with %v", err)
	}
	if id, err := git.ParseObjectID(0, sum); !errors.Is(err, git.ErrNoFormat) {
		t.Errorf("an id read in no format = %v, %v; want ErrNoFormat", id, err)
	}
}

// held is every file under dir with its size and modification time, so a
// test sees whether anything was written there.
func held(t *testing.T, dir string) []string {
	t.Helper()
	var held []string
	err := filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		held = append(held, fmt.Sprintf("%s %d %v", p, info.Size(), info.ModTime()))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return held
}

// A repository directory that is a link is not lit's repository, wherever
// it points: every read and write through it is refused, and nothing is
// written where it points.
func TestARepositoryDirThatIsALinkIsRefused(t *testing.T) {
	t.Run("a link to another bare repository", func(t *testing.T) {
		g := gittest.Runner(t)
		other := filepath.Join(gittest.Physical(t), "other")
		if _, err := gittest.Put(repositoryAt(g, other), []byte("theirs")); err != nil {
			t.Fatal(err)
		}
		before := held(t, other)
		at := filepath.Join(gittest.Physical(t), "repo")
		if err := os.Symlink(other, at); err != nil {
			t.Fatal(err)
		}
		repo := repositoryAt(g, at)
		if l, err := repo.List("refs/"); !errors.As(err, new(files.NotTheDirectory)) {
			t.Errorf("a list through the link = %+v, %v; want NotTheDirectory", l, err)
		}
		if _, err := gittest.Put(repo, []byte("mine")); !errors.As(err, new(files.NotTheDirectory)) {
			t.Errorf("a write through the link failed with %v; want NotTheDirectory", err)
		}
		if after := held(t, other); !slices.Equal(after, before) {
			t.Errorf("the repository the link points to holds %v; want %v, as it was", after, before)
		}
	})
	t.Run("a link to nothing", func(t *testing.T) {
		nothing := filepath.Join(gittest.Physical(t), "nothing")
		at := filepath.Join(gittest.Physical(t), "repo")
		if err := os.Symlink(nothing, at); err != nil {
			t.Fatal(err)
		}
		if _, err := gittest.Put(repositoryAt(gittest.Runner(t), at), []byte("mine")); !errors.As(err, new(files.NotTheDirectory)) {
			t.Errorf("the first write through a link to nothing failed with %v; want NotTheDirectory", err)
		}
		if _, err := os.Lstat(nothing); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("the link's target is there after the write: %v; want nothing made", err)
		}
		if info, err := os.Lstat(at); err != nil || info.Mode().Type() != fs.ModeSymlink {
			t.Errorf("the link is %v, %v after the write; want it still the link, never renamed over", info, err)
		}
	})
}

// lit's repository's entry in the directory it is in is fsynced before a
// write is handed the repository, on every path: a repository the write
// renamed into place, one whose rename it lost to another create, and one
// it found there. The lost rename is forced by the gate: a second create
// makes the repository between this one's look and its rename.
func TestTheRepositorysEntryIsSyncedOnEveryWrite(t *testing.T) {
	g := gittest.Runner(t)
	in := filepath.Join(gittest.Physical(t), "store")
	at := func(seams filestest.Seams) files.Dir {
		return filestest.Seamed(files.DirAt(filepath.Dir(in), files.MustName(filepath.Base(in))), seams).Sub(files.MustName("repo"))
	}
	// synced fails t unless a write through seams synced in, last.
	synced := func(t *testing.T, path string, seams func(*filestest.Syncs) filestest.Seams) {
		t.Helper()
		var syncs filestest.Syncs
		gittest.PutBlob(t, g.RepositoryAt(at(seams(&syncs))), []byte(path))
		if seen := syncs.Seen(); len(seen) == 0 || seen[len(seen)-1] != in {
			t.Errorf("a write %s synced %v; want %s, the directory the repository is in, last", path, seen, in)
		}
	}
	recording := func(s *filestest.Syncs) filestest.Seams { return filestest.Recording(s, filestest.Passing) }
	synced(t, "renaming it into place", recording)
	synced(t, "finding it there", recording)

	// the creating walk of the directory it is in is the first look at
	// it, since the check before finds no directory and looks at nothing;
	// the other create runs there, after this one found no repository and
	// before its rename, which it then loses
	in = filepath.Join(gittest.Physical(t), "store")
	other := false
	synced(t, "losing its rename", func(s *filestest.Syncs) filestest.Seams {
		seams := recording(s)
		seams.Gate = func(path string) {
			if path == in && !other {
				other = true
				gittest.PutBlob(t, g.RepositoryAt(at(filestest.OS())), []byte("the other create"))
			}
		}
		return seams
	})
	if !other {
		t.Error("the other create never ran; want it inside this one's creating walk")
	}
}

// A sync of the repository's entry that failed is never tried again: every
// later write in the process is refused with it, since a second sync may
// pass over what the first dropped.
func TestAFailedSyncOfTheRepositorysEntryRefusesEveryLaterWrite(t *testing.T) {
	g := gittest.Runner(t)
	in := filepath.Join(gittest.Physical(t), "store")
	errSync := errors.New("the disk refused the fsync")
	var syncs filestest.Syncs
	seams := filestest.Recording(&syncs, func(path string) error {
		if path == in {
			return errSync
		}
		return nil
	})
	repo := g.RepositoryAt(filestest.Seamed(files.DirAt(filepath.Dir(in), files.MustName(filepath.Base(in))), seams).Sub(files.MustName("repo")))
	for _, write := range []string{"first", "second"} {
		if _, err := gittest.Put(repo, []byte(write)); !errors.Is(err, errSync) {
			t.Errorf("the %s write failed with %v; want the failed sync", write, err)
		}
	}
	synced := 0
	for _, p := range syncs.Seen() {
		if p == in {
			synced++
		}
	}
	if synced != 1 {
		t.Errorf("the entry was synced %d times over %v; want once, never again after it failed", synced, syncs.Seen())
	}
}
