package store_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/store/storetest"
)

func memory(*testing.T) store.Ports {
	repo := git.NewMemory()
	return store.Ports{Files: store.NewMemoryFiles(), Objects: repo, Refs: repo, Entropy: storetest.Entropy(1)}
}

// gitdir is a fresh clone's common dir on disk, made by git init, and its
// store's ports as the composition root builds them.
func gitdir(t *testing.T) store.Ports {
	return store.DiskPorts(clone(t), storetest.Entropy(2))
}

func TestStoreContract(t *testing.T) {
	t.Run("memory", func(t *testing.T) { storetest.Store(t, memory) })
	t.Run("gitdir", func(t *testing.T) { storetest.Store(t, gitdir) })
}

func TestFilesContract(t *testing.T) {
	t.Run("disk", func(t *testing.T) {
		storetest.Files(t, func(t *testing.T) store.Files { return store.FilesAt(filepath.Join(t.TempDir(), "store")) })
	})
	t.Run("memory", func(t *testing.T) {
		storetest.Files(t, func(*testing.T) store.Files { return store.NewMemoryFiles() })
	})
}

// run runs the git binary, the oracle and fixture builder of these tests,
// in dir, with no user or system config. lit's code never runs it; a
// machine without it fails these tests rather than skipping them.
func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("these tests need the git binary as their oracle: %v", err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// clone is a fresh repository with one commit, located as lit2 locates it.
func clone(t *testing.T) git.Checkout {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "commit", "-q", "--allow-empty", "-m", "first")
	c, err := git.Locate(dir)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
