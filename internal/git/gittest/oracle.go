package gittest

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/git"
)

// Git runs the git binary in dir and is its output, failing t when git is
// missing or refuses. It is the oracle lit's own code never calls.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	out, err := Ask(t, dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Ask runs the git binary in dir and is its output, or its refusal with
// what it said, for a test whose answer may be that git refuses. It fails
// t only when git is missing. Its environment is fixed, not inherited, so
// no config, hook or GIT_DIR of the caller's reaches what it reports.
func Ask(t testing.TB, dir string, args ...string) (string, error) {
	t.Helper()
	return ask(t, dir, "", args...)
}

// Exit is the status git exits with running args in dir: 0 when it
// succeeds, else the status it refuses with. It fails t when git does not
// run, so a refusal a test names by its status is git's, never a git that
// never started.
func Exit(t testing.TB, dir string, args ...string) int {
	t.Helper()
	_, err := Ask(t, dir, args...)
	var exit *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exit):
		return exit.ExitCode()
	}
	t.Fatal(err)
	return 0
}

// GitIn is Git with stdin given.
func GitIn(t testing.TB, dir, stdin string, args ...string) string {
	t.Helper()
	out, err := ask(t, dir, stdin, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func ask(t testing.TB, dir, stdin string, args ...string) (string, error) {
	t.Helper()
	bin, env, err := found()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, args...)
	cmd.Dir, cmd.Stdin = dir, strings.NewReader(stdin)
	cmd.Env = append(env, "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s in %s: %w\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// Env is the environment a test's lit2 runs git under: the git the
// oracle runs, and no config of the machine's, so what a test sees is what
// git does and not what this machine's config makes it do.
func Env(t testing.TB) []string {
	t.Helper()
	env, err := Environ()
	if err != nil {
		t.Fatal(err)
	}
	return env
}

// Environ is Env for a caller with no test to fail, as TestMain is.
func Environ() ([]string, error) {
	_, env, err := found()
	return env, err
}

// found is the git binary on PATH and the environment it runs under, for
// the oracle and a test's lit2 alike: one recipe `[LAW:one-source-of-truth]`.
func found() (string, []string, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return "", nil, fmt.Errorf("these tests need the git binary: %w", err)
	}
	return bin, []string{"PATH=" + filepath.Dir(bin), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}, nil
}

// Runner is lit2's git as a test runs it.
func Runner(t testing.TB, env ...string) *git.Git {
	t.Helper()
	return git.Find(append(Env(t), env...))
}

// Physical is a temporary directory by its physical path, as Locate and
// git both report it.
func Physical(t testing.TB) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// Clone is a fresh repository with one commit, located as lit2 locates it.
func Clone(t testing.TB) git.Checkout {
	t.Helper()
	dir := Physical(t)
	Git(t, dir, "init", "-q", "-b", "main")
	Git(t, dir, "commit", "-q", "--allow-empty", "-m", "first")
	c, err := Runner(t).Locate(dir)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// NoCheckout is a directory outside every checkout, and the git that
// sees it so: a repository is planted above it, and a ceiling between
// them, so it is outside a checkout whatever holds the test's temporary
// directory, and a git that ignored the ceiling would find the planted
// one.
func NoCheckout(t testing.TB) (dir string, g *git.Git) {
	t.Helper()
	planted := Physical(t)
	Git(t, planted, "init", "-q")
	dir = filepath.Join(planted, "outside")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, Runner(t, Ceiling(dir))
}

// GitPath is the absolute path git keeps path at in the repository of
// dir, as git resolves it, so a test names git's layout by git's own
// answer.
func GitPath(t testing.TB, dir, path string) string {
	t.Helper()
	return Git(t, dir, "rev-parse", "--path-format=absolute", "--git-path", path)
}

// Ceiling is the variable that stops git's walk up from dir at the
// directory dir is in, as NoCheckout's git has it.
func Ceiling(dir string) string { return "GIT_CEILING_DIRECTORIES=" + filepath.Dir(dir) }

// ObjectsDir is the directory git keeps the objects of the repository at
// repo in. It, LooseObject and LooseRef are the oracle's one spelling of
// git's layout.
func ObjectsDir(repo string) string { return filepath.Join(repo, "objects") }

// LooseObject is where git's files backend keeps id loose in the
// repository at repo: its objects directory, then the id's first two hex
// digits, then the rest.
func LooseObject(repo string, id git.ObjectID) string {
	hex := id.String()
	return filepath.Join(ObjectsDir(repo), hex[:2], hex[2:])
}

// LooseRef is where git's files backend keeps the ref n loose in the
// repository at repo; of a prefix of refs, n ending in /, it is the
// directory they are kept in.
func LooseRef(repo string, n git.RefName) string {
	return filepath.Join(repo, filepath.FromSlash(string(n)))
}
