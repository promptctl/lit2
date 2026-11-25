//go:build integration

package git_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
)

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
	g := gittest.Runner(t)
	c, err := g.Locate(linked)
	if err != nil {
		t.Fatal(err)
	}
	got, err := g.CodeAt(c).AtHead("f")
	if want := (git.Tracked{Blob: git.HashBlob(git.SHA1, []byte("side\n"))}); err != nil || got != want {
		t.Errorf("the linked worktree's f = %#v, %v; want its own commit's %#v", got, err, want)
	}
}

// TestASHA256CodeRepositoryIsRead: git reads either format, so a SHA-256
// code repository's blobs are its own SHA-256 ids.
func TestASHA256CodeRepositoryIsRead(t *testing.T) {
	dir := gittest.Physical(t)
	gittest.Git(t, dir, "init", "-q", "--object-format=sha256")
	if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Git(t, dir, "add", "f")
	gittest.Git(t, dir, "commit", "-q", "-m", "f")
	g := gittest.Runner(t)
	c, err := g.Locate(dir)
	if err != nil {
		t.Fatal(err)
	}
	code, id := g.CodeAt(c), git.HashBlob(git.SHA256, []byte("x\n"))
	if got, err := code.AtHead("f"); err != nil || got != (git.Tracked{Blob: id}) {
		t.Errorf("AtHead in a sha256 repository = %#v, %v; want %v", got, err, id)
	}
	if ever, err := code.EverAt("f", id); err != nil || !ever {
		t.Errorf("EverAt in a sha256 repository = %v, %v; want true", ever, err)
	}
}

// TestABlobOnlyOnAMergedBranchWasEverAtThePath: the walk covers every
// commit HEAD reaches, a side branch whose change the merge did not keep
// included.
func TestABlobOnlyOnAMergedBranchWasEverAtThePath(t *testing.T) {
	repo := gittest.NewRepo(t)
	repo.Commit(t, map[string][]byte{"f": []byte("main\n")})
	gittest.Git(t, repo.Dir, "checkout", "-q", "-b", "side")
	repo.Commit(t, map[string][]byte{"f": []byte("side\n")})
	gittest.Git(t, repo.Dir, "checkout", "-q", "main")
	gittest.Git(t, repo.Dir, "merge", "-q", "-s", "ours", "-m", "merge", "side")
	if ever, err := repo.EverAt("f", git.HashBlob(git.SHA1, []byte("side\n"))); err != nil || !ever {
		t.Errorf("EverAt(the side branch's blob) = %v, %v; want true", ever, err)
	}
}

// TestASymlinkAtThePathIsNoFile: HEAD's blob for a symlink is its target's
// name, never the bytes the work tree reads through it.
func TestASymlinkAtThePathIsNoFile(t *testing.T) {
	repo := gittest.NewRepo(t)
	repo.Commit(t, map[string][]byte{"real.toml": []byte("prefix = \"a\"\n")})
	if err := os.Symlink("real.toml", filepath.Join(repo.Dir, "link.toml")); err != nil {
		t.Fatal(err)
	}
	gittest.Git(t, repo.Dir, "add", "link.toml")
	gittest.Git(t, repo.Dir, "commit", "-q", "-m", "link")
	if got, err := repo.AtHead("link.toml"); err != nil || got != (git.NotAFile{Mode: "120000"}) {
		t.Errorf("AtHead(a symlink) = %#v, %v; want NotAFile, mode 120000", got, err)
	}
}

// A HEAD whose branch names an object the repository does not hold is a
// damaged repository, git's error, never a branch with no commit.
func TestAHeadNamingAMissingObjectIsAnError(t *testing.T) {
	repo := gittest.NewRepo(t)
	// the control: the same repository, its HEAD naming a commit it holds,
	// reads
	repo.Commit(t, map[string][]byte{"f": nil})
	if got, err := repo.AtHead("f"); err != nil {
		t.Fatalf("AtHead over a whole HEAD = %#v, %v", got, err)
	}
	const missing = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	if err := os.WriteFile(gittest.LooseRef(filepath.Join(repo.Dir, ".git"), "refs/heads/main"), []byte(missing+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.AtHead("f"); !errors.Is(err, git.ErrHeadMissing) || !strings.Contains(err.Error(), missing) {
		t.Errorf("AtHead over a missing HEAD = %#v, %v; want ErrHeadMissing, naming %s", got, err, missing)
	}
	if ever, err := repo.EverAt("f", git.HashBlob(git.SHA1, nil)); !errors.Is(err, git.ErrHeadMissing) {
		t.Errorf("EverAt over a missing HEAD = %v, %v; want ErrHeadMissing", ever, err)
	}
}

// A replace ref or graft that cuts HEAD's history short hides no blob from
// EverAt: the history read is the one the repository holds.
func TestAGraftHidesNoBlobFromTheHistory(t *testing.T) {
	repo := gittest.NewRepo(t)
	repo.Commit(t, map[string][]byte{"f": []byte("one\n")})
	repo.Commit(t, map[string][]byte{"f": []byte("two\n")})
	head := gittest.Git(t, repo.Dir, "rev-parse", "HEAD")
	gittest.Git(t, repo.Dir, "replace", "--graft", head)
	if err := os.WriteFile(filepath.Join(repo.Dir, ".git", "info", "grafts"), []byte(head+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if ever, err := repo.EverAt("f", git.HashBlob(git.SHA1, []byte("one\n"))); err != nil || !ever {
		t.Errorf("EverAt(the blob before the graft) = %v, %v; want true", ever, err)
	}
}

// TestAShallowCloneSaysWhatItCannotKnow: a blob before the boundary is
// found; one the walk must pass the boundary for is unknown, refused naming
// the shallow clone, never answered no.
func TestAShallowCloneSaysWhatItCannotKnow(t *testing.T) {
	const path = ".lit/workspace.toml"
	one, two := []byte("prefix = \"a\"\n"), []byte("prefix = \"b\"\n")
	repo := gittest.NewRepo(t)
	repo.Commit(t, map[string][]byte{path: one})
	repo.Commit(t, map[string][]byte{path: two})
	shallow := filepath.Join(gittest.Physical(t), "shallow")
	gittest.Git(t, repo.Dir, "clone", "-q", "--depth", "1", "file://"+repo.Dir, shallow)
	g := gittest.Runner(t)
	c, err := g.Locate(shallow)
	if err != nil {
		t.Fatal(err)
	}
	code := g.CodeAt(c)
	if ever, err := code.EverAt(path, git.HashBlob(git.SHA1, two)); err != nil || !ever {
		t.Errorf("EverAt(HEAD's blob) in a shallow clone = %v, %v; want true", ever, err)
	}
	if ever, err := code.EverAt(path, git.HashBlob(git.SHA1, one)); !errors.Is(err, git.ErrShallow) {
		t.Errorf("EverAt(a blob past the boundary) = %v, %v; want ErrShallow", ever, err)
	} else if !strings.Contains(err.Error(), "shallow") {
		t.Errorf("EverAt(a blob past the boundary) = %v; want it to say the clone is shallow", err)
	}
}
