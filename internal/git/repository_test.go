package git_test

import (
	"errors"
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
		gittest.Objects(t, func(t *testing.T) git.Objects { return git.RepositoryAt(filepath.Join(t.TempDir(), "repo")) })
	})
	t.Run("memory", func(t *testing.T) {
		gittest.Objects(t, func(*testing.T) git.Objects { return git.NewMemory() })
	})
}

func TestRefsContract(t *testing.T) {
	t.Run("repository", func(t *testing.T) {
		gittest.Refs(t, func(t *testing.T) git.Refs { return git.RepositoryAt(filepath.Join(t.TempDir(), "repo")) })
	})
	t.Run("memory", func(t *testing.T) {
		gittest.Refs(t, func(*testing.T) git.Refs { return git.NewMemory() })
	})
}

// agrees holds Locate from dir to what git rev-parse says of it.
func agrees(t *testing.T, dir string) {
	t.Helper()
	got, err := git.Locate(dir)
	if err != nil {
		t.Fatalf("Locate(%s): %v", dir, err)
	}
	want := strings.Split(gittest.Git(t, dir, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-dir", "--git-common-dir"), "\n")
	if have := []string{got.WorkTree, got.GitDir, got.CommonDir}; !slices.Equal(have, want) {
		t.Errorf("Locate(%s) = %v; git says %v", dir, have, want)
	}
}

func TestLocateAgreesWithGit(t *testing.T) {
	root := gittest.Physical(t)
	main, linked, sub := filepath.Join(root, "main"), filepath.Join(root, "linked"), filepath.Join(root, "sub")
	gittest.Git(t, root, "init", "-q", "-b", "main", main)
	gittest.Git(t, main, "commit", "-q", "--allow-empty", "-m", "first")
	gittest.Git(t, main, "worktree", "add", "-q", linked)
	gittest.Git(t, root, "init", "-q", "-b", "main", sub)
	gittest.Git(t, sub, "commit", "-q", "--allow-empty", "-m", "first")
	gittest.Git(t, main, "-c", "protocol.file.allow=always", "submodule", "add", "-q", sub, "sub")
	for _, dir := range []string{main, linked, filepath.Join(main, "sub")} {
		agrees(t, dir)
	}
}

func TestReadObjectFormatAgreesWithGit(t *testing.T) {
	for _, format := range []git.ObjectFormat{git.SHA1, git.SHA256} {
		dir := gittest.Physical(t)
		gittest.Git(t, dir, "init", "-q", "--object-format="+format.String())
		c, err := git.Locate(dir)
		if err != nil {
			t.Fatal(err)
		}
		got, err := git.ReadObjectFormat(c.CommonDir)
		if want := gittest.Git(t, dir, "rev-parse", "--show-object-format"); err != nil || got.String() != want || got != format {
			t.Errorf("ReadObjectFormat in a %s repository = %v, %v; git says %s", format, got, err, want)
		}
	}
}

func TestRepositoryListsPackedAndLooseRefs(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	repo := git.RepositoryAt(dir)
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
	heads, bad, err := repo.List("refs/lit/")
	want := []git.RefHead{{Name: "refs/lit/log/both", Target: other}, {Name: "refs/lit/log/loose", Target: other}, {Name: "refs/lit/log/packed", Target: blob}}
	if err != nil || len(bad) != 0 || !slices.Equal(heads, want) {
		t.Errorf("List = %v, %v, %v; want %v, the loose ref over its packed copy", heads, bad, err, want)
	}
	if got := gittest.Git(t, dir, "rev-parse", "refs/lit/log/both"); got != other.String() {
		t.Errorf("git reads the replaced ref as %s; want %s", got, other)
	}
}

func TestATornRefIsBadAndTheRestRead(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	repo := git.RepositoryAt(dir)
	blob, _, err := repo.PutBlob([]byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace("refs/lit/log/good", blob); err != nil {
		t.Fatal(err)
	}
	// as a power loss after a rename and before the data reached the disk
	if err := os.WriteFile(filepath.Join(dir, "refs", "lit", "log", "torn"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	heads, bad, err := repo.List("refs/lit/")
	if err != nil || !slices.Equal(heads, []git.RefHead{{Name: "refs/lit/log/good", Target: blob}}) || len(bad) != 1 || bad[0].Name != "refs/lit/log/torn" {
		t.Errorf("List = %v, %v, %v; want the good ref and the torn one bad", heads, bad, err)
	}
}

func TestReadingCreatesNoRepository(t *testing.T) {
	dir := filepath.Join(gittest.Physical(t), "repo")
	repo := git.RepositoryAt(dir)
	if _, err := repo.Blob(gittest.ID(t, "ce013625030ba8dba906f756967f9e9ca394464a")); !errors.As(err, new(git.MissingObject)) {
		t.Errorf("reading an absent repository failed with %v; want MissingObject", err)
	}
	if heads, bad, err := repo.List("refs/lit/"); err != nil || len(heads)+len(bad) != 0 {
		t.Errorf("listing an absent repository = %v, %v, %v; want nothing", heads, bad, err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("reading created %s: %v", dir, err)
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
}
