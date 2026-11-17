package git_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
)

func TestCodeContract(t *testing.T) {
	t.Run("repository", func(t *testing.T) {
		gittest.Code(t, func(t *testing.T) gittest.Commits { return gittest.NewRepo(t) })
	})
	t.Run("memory", func(t *testing.T) {
		gittest.Code(t, func(*testing.T) gittest.Commits { return gittest.Memory{CodeMemory: &git.CodeMemory{}} })
	})
}

// TestALinkedWorktreeReadsItsOwnHead: a linked worktree's HEAD is its own
// branch, read through its git dir, its objects through the common dir.
func TestALinkedWorktreeReadsItsOwnHead(t *testing.T) {
	repo := gittest.NewRepo(t)
	repo.Commit(t, map[string][]byte{"f": []byte("main\n")})
	linked := filepath.Join(gittest.Physical(t), "linked")
	gittest.Git(t, repo.Dir, "worktree", "add", "-q", "-b", "side", linked)
	if err := os.WriteFile(filepath.Join(linked, "f"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Git(t, linked, "commit", "-q", "-am", "side")
	c, err := git.Locate(linked)
	if err != nil {
		t.Fatal(err)
	}
	got, err := git.CodeAt(c, git.SHA1).AtHead("f")
	if want := (git.Tracked{Blob: git.HashBlob(git.SHA1, []byte("side\n"))}); err != nil || got != want {
		t.Errorf("the linked worktree's f = %#v, %v; want its own commit's %#v", got, err, want)
	}
}

// TestASHA256CodeRepositoryIsRefusedByName: go-git v5 reads no SHA-256
// repository, so one is refused, saying so, never misread.
func TestASHA256CodeRepositoryIsRefusedByName(t *testing.T) {
	dir := gittest.Physical(t)
	gittest.Git(t, dir, "init", "-q", "--object-format=sha256")
	c, err := git.Locate(dir)
	if err != nil {
		t.Fatal(err)
	}
	code := git.CodeAt(c, git.SHA256)
	if _, err := code.AtHead("f"); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Errorf("AtHead on a sha256 repository = %v; want a refusal naming sha256", err)
	}
	if _, err := code.EverAt("f", git.HashBlob(git.SHA256, nil)); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Errorf("EverAt on a sha256 repository = %v; want a refusal naming sha256", err)
	}
}

// TestAShallowCloneSaysWhatItCannotKnow: a blob before the boundary is
// found; one not found is unknown, refused naming the shallow clone, never
// answered no.
func TestAShallowCloneSaysWhatItCannotKnow(t *testing.T) {
	const path = ".lit/workspace.toml"
	one, two := []byte("prefix = \"a\"\n"), []byte("prefix = \"b\"\n")
	repo := gittest.NewRepo(t)
	repo.Commit(t, map[string][]byte{path: one})
	repo.Commit(t, map[string][]byte{path: two})
	shallow := filepath.Join(gittest.Physical(t), "shallow")
	gittest.Git(t, repo.Dir, "clone", "-q", "--depth", "1", "file://"+repo.Dir, shallow)
	c, err := git.Locate(shallow)
	if err != nil {
		t.Fatal(err)
	}
	code := git.CodeAt(c, git.SHA1)
	if ever, err := code.EverAt(path, git.HashBlob(git.SHA1, two)); err != nil || !ever {
		t.Errorf("EverAt(HEAD's blob) in a shallow clone = %v, %v; want true", ever, err)
	}
	if ever, err := code.EverAt(path, git.HashBlob(git.SHA1, one)); err == nil || !strings.Contains(err.Error(), "shallow") {
		t.Errorf("EverAt(a blob past the boundary) = %v, %v; want a refusal naming the shallow clone", ever, err)
	}
}

// TestASymlinkAtThePathIsNoFile: HEAD's blob for a symlink is its target's
// text, never the bytes the work tree reads through it, so it is no file.
func TestASymlinkAtThePathIsNoFile(t *testing.T) {
	repo := gittest.NewRepo(t)
	repo.Commit(t, map[string][]byte{"real.toml": []byte("prefix = \"a\"\n")})
	if err := os.Symlink("real.toml", filepath.Join(repo.Dir, "link.toml")); err != nil {
		t.Fatal(err)
	}
	gittest.Git(t, repo.Dir, "add", "link.toml")
	gittest.Git(t, repo.Dir, "commit", "-q", "-m", "link")
	if got, err := repo.AtHead("link.toml"); err != nil || got != (git.Untracked{}) {
		t.Errorf("AtHead(a symlink) = %#v, %v; want Untracked", got, err)
	}
}
