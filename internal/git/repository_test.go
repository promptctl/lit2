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
	for _, n := range gittest.Refused {
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
}

// A git missing from PATH is one loud error from every spawn, and nothing
// ran.
func TestAGitMissingFromPathIsOneLoudError(t *testing.T) {
	g := git.Find([]string{"PATH=" + t.TempDir()})
	_, errLocate := g.Locate(t.TempDir())
	_, _, errPut := g.RepositoryAt(filepath.Join(t.TempDir(), "repo")).PutBlob(nil)
	for _, err := range []error{errLocate, errPut} {
		if err == nil || !strings.Contains(err.Error(), "git is not on PATH") {
			t.Errorf("a spawn with no git failed with %v; want the one error naming PATH", err)
		}
	}
	if spawns := g.Spawns(); len(spawns) != 0 {
		t.Errorf("with no git %v ran", spawns)
	}
}

// Every git process is on the record: its arguments, its exit and its
// stderr, the long-lived batch included once it ends.
func TestEverySpawnIsRecorded(t *testing.T) {
	g := gittest.Runner(t)
	dir := gittest.Physical(t)
	if _, err := g.Locate(dir); err == nil {
		t.Fatal("Locate outside a repository succeeded")
	}
	repo := g.RepositoryAt(filepath.Join(dir, "repo"))
	id, _, err := repo.PutBlob([]byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Blob(id); err != nil {
		t.Fatal(err)
	}
	spawns := g.Spawns()
	var commands []string
	for _, s := range spawns {
		commands = append(commands, s.Args[slices.IndexFunc(s.Args, func(a string) bool { return !strings.HasPrefix(a, "-") && !strings.Contains(a, "=") })])
	}
	// the read is go-git's, in process, so it spawns nothing
	if want := []string{"rev-parse", "init", "config", "config", "mktree", "hash-object"}; !slices.Equal(commands, want) {
		t.Errorf("the spawns ran %v; want %v", commands, want)
	}
	if spawns[0].Exit != 128 || !strings.Contains(spawns[0].Stderr, "not a git repository") || spawns[len(spawns)-1].Exit != 0 {
		t.Errorf("the spawns were %+v; want the refusal with git's stderr and the write's success", spawns)
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
	blob, _, err := repo.PutBlob([]byte("hello\n"))
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := repo.PutBlob(nil)
	if err != nil {
		t.Fatal(err)
	}
	gittest.Git(t, dir, "update-ref", "refs/lit/log/packed", blob.String())
	gittest.Git(t, dir, "update-ref", "refs/lit/log/both", blob.String())
	gittest.Git(t, dir, "pack-refs", "--all")
	if err := repo.Replace("refs/lit/log/both", other); err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace("refs/lit/log/loose", other); err != nil {
		t.Fatal(err)
	}
	heads, err := repo.List("refs/lit/")
	want := []git.RefHead{{Name: "refs/lit/log/both", Target: other}, {Name: "refs/lit/log/loose", Target: other}, {Name: "refs/lit/log/packed", Target: blob}}
	if err != nil || !slices.Equal(heads, want) {
		t.Errorf("List = %v, %v; want %v, the loose ref over its packed copy", heads, err, want)
	}
}

// A ref broken by something other than git, the one writer, which writes
// every ref whole, fails the listing loudly, naming lit's repository.
func TestABrokenRefFailsTheListing(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	g := gittest.Runner(t)
	repo := g.RepositoryAt(dir)
	blob, _, err := repo.PutBlob([]byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace("refs/lit/log/good", blob); err != nil {
		t.Fatal(err)
	}
	for _, torn := range []string{"", "torn", "ref: refs/lit/log/good"[:9]} {
		if err := os.WriteFile(filepath.Join(dir, "refs", "lit", "log", "torn"), []byte(torn), 0o644); err != nil {
			t.Fatal(err)
		}
		if heads, err := repo.List("refs/lit/"); err == nil || !strings.Contains(err.Error(), dir) {
			t.Errorf("with a ref file holding %q, List = %v, %v; want a failure naming %s", torn, heads, err, dir)
		}
	}
}

// git writes a ref as its lock file, renamed into place: a lock, empty or
// half-written, is never a ref, so a listing during a write sees the ref
// as it was.
func TestALockFileIsNoRef(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	repo := gittest.Runner(t).RepositoryAt(dir)
	blob, _, err := repo.PutBlob([]byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace("refs/lit/log/a", blob); err != nil {
		t.Fatal(err)
	}
	for _, lock := range []string{"", blob.String() + "\n"} {
		for _, n := range []string{"a.lock", "b.lock"} {
			if err := os.WriteFile(filepath.Join(dir, "refs", "lit", "log", n), []byte(lock), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if heads, err := repo.List("refs/lit/"); err != nil || !slices.Equal(heads, []git.RefHead{{Name: "refs/lit/log/a", Target: blob}}) {
			t.Errorf("with lock files holding %q, List = %v, %v; want refs/lit/log/a alone", lock, heads, err)
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
	if heads, err := repo.List("refs/lit/"); err != nil || len(heads) != 0 {
		t.Errorf("listing an absent repository = %v, %v; want nothing", heads, err)
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
	_, _, errPut := repo.PutBlob([]byte("x"))
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
	blob, _, err := repo.PutBlob([]byte("x"))
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
	foreign := gittest.GitIn(t, dir, "m\n", "commit-tree", tree.String())
	child := gittest.GitIn(t, dir, "m\n", "commit-tree", "-p", lit.String(), tree.String())
	for _, c := range []string{foreign, child} {
		if got, err := repo.Commit(gittest.ID(t, c)); err == nil {
			t.Errorf("Commit(%s) read a commit lit did not write as %+v", c, got)
		}
	}
	exec := gittest.GitIn(t, dir, "100755 blob "+blob.String()+"\ta\x00", "mktree", "-z")
	if got, err := repo.Tree(gittest.ID(t, exec)); err == nil {
		t.Errorf("Tree(%s) read a tree lit did not write as %v", exec, got)
	}
	if _, err := gittest.Ask(t, dir, "fsck", "--strict"); err != nil {
		t.Errorf("git fsck --strict refuses what lit wrote: %v", err)
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
