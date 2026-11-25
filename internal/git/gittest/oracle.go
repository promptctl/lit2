package gittest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
)

// The contract suites' real arms, unexported; integration.go, built with
// the integration tag alone, names them for the tests built with it, so a
// fast test cannot call one by name.

// isolation is the environment every git a test runs is under, the
// oracle's and lit2's own: the git on PATH, and no config of the
// machine's, so what a test sees is what git does and not what this
// machine's config makes it do.
func isolation(t testing.TB) []string {
	t.Helper()
	return isolated(binary(t))
}

// isolated is the isolation for the git binary at bin.
func isolated(bin string) []string {
	return append([]string{"PATH=" + filepath.Dir(bin)}, git.NoConfig()...)
}

// binary is the git binary on the test's PATH, failing t without one.
func binary(t testing.TB) string {
	t.Helper()
	bin, err := lookGit()
	if err != nil {
		t.Fatal(err)
	}
	return bin
}

// lookGit is the git binary on PATH, the one git every test and the
// smoke build run.
func lookGit() (string, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("these tests need the git binary: %w", err)
	}
	return bin, nil
}

// ask runs the git binary in dir, under the isolation and a fixed
// identity, and is its output, or its refusal with what it said. It fails
// t only when git is missing. It is the oracle lit's own code never calls.
func ask(t testing.TB, dir, stdin string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(binary(t), args...)
	cmd.Dir, cmd.Stdin = dir, strings.NewReader(stdin)
	cmd.Env = append(isolation(t), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s in %s: %w\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// oracle is ask with no stdin, failing t when git refuses.
func oracle(t testing.TB, dir string, args ...string) string {
	t.Helper()
	return oracleIn(t, dir, "", args...)
}

// oracleIn is ask, failing t when git refuses.
func oracleIn(t testing.TB, dir, stdin string, args ...string) string {
	t.Helper()
	out, err := ask(t, dir, stdin, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// runner is lit2's git as a test runs it, under the isolation and env.
func runner(t testing.TB, env ...string) *git.Git {
	t.Helper()
	return git.Find(append(isolation(t), env...))
}

// physical is a temporary directory by its physical path, as Locate and
// git both report it.
func physical(t testing.TB) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// repositoryAt is lit's repository at path, it and the directory it is in
// checked from the directory above.
func repositoryAt(g *git.Git, path string) git.Repository {
	in := filepath.Dir(path)
	return g.RepositoryAt(files.DirAt(filepath.Dir(in), files.MustName(filepath.Base(in))).Sub(files.MustName(filepath.Base(path))), files.NewDurability())
}

// freshRepository is lit's repository in a fresh directory, made by its
// first write.
func freshRepository(t *testing.T) git.Repository {
	return repositoryAt(runner(t), filepath.Join(t.TempDir(), "repo"))
}

// repo is a code repository on disk, committed to through the git binary.
type repo struct {
	git.Code
	Dir string
}

// newRepo is an empty code repository on disk, read as lit2 reads one.
func newRepo(t *testing.T) repo {
	t.Helper()
	dir := physical(t)
	oracle(t, dir, "init", "-q", "-b", "main")
	g := runner(t)
	c, err := g.Locate(dir)
	if err != nil {
		t.Fatal(err)
	}
	return repo{Code: g.CodeAt(c), Dir: dir}
}

// Commit writes files as the work tree's whole content and commits it.
func (r repo) Commit(t *testing.T, files map[string][]byte) {
	t.Helper()
	oracle(t, r.Dir, "rm", "-rqf", "--ignore-unmatch", ".")
	for p, b := range files {
		full := filepath.Join(r.Dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, b, 0o644); err != nil {
			t.Fatal(err)
		}
		oracle(t, r.Dir, "add", "--", p)
	}
	oracle(t, r.Dir, "commit", "-q", "--allow-empty", "-m", "commit")
}
