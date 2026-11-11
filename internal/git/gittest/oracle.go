package gittest

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/git"
)

// Git runs the git binary in dir and is its output, failing t when git is
// missing or refuses. It is the oracle lit's own code never calls. Its
// environment is fixed, not inherited, so no config, hook or GIT_DIR of
// the caller's reaches what it reports.
func Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("these tests need the git binary as their oracle: %v", err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	cmd.Env = []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimRight(string(out), "\n")
}

// Physical is a temporary directory by its physical path, as Locate and
// git both report it.
func Physical(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// Clone is a fresh repository with one commit, located as lit2 locates it.
func Clone(t *testing.T) git.Checkout {
	t.Helper()
	dir := Physical(t)
	Git(t, dir, "init", "-q", "-b", "main")
	Git(t, dir, "commit", "-q", "--allow-empty", "-m", "first")
	c, err := git.Locate(dir)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
