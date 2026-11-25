//go:build integration

package gittest

import (
	"errors"
	"fmt"
	"io/fs"
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
	return oracle(t, dir, args...)
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

// Config is the value git config reads with args, or false when the key
// is unset. git config exits 1 both for an unset key and for one its
// grammar refuses, so an exit 1 is asked again with --default, which
// answers an unset key and still refuses a malformed one; any other
// refusal fails t, so a config git cannot read is never taken as a key
// with no value.
func Config(t testing.TB, dir string, args ...string) (string, bool) {
	t.Helper()
	out, err := Ask(t, dir, append([]string{"config"}, args...)...)
	var exit *exec.ExitError
	switch {
	case err == nil:
		return out, true
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		if _, err := Ask(t, dir, append([]string{"config", "--default="}, args...)...); err != nil {
			t.Fatalf("git config %q is no key git reads: %v", args, err)
		}
		return "", false
	}
	t.Fatal(err)
	return "", false
}

// GitIn is Git with stdin given.
func GitIn(t testing.TB, dir, stdin string, args ...string) string {
	t.Helper()
	return oracleIn(t, dir, stdin, args...)
}

// Env is the environment a test's lit2 runs git under, the one the oracle
// runs under.
func Env(t testing.TB) []string {
	t.Helper()
	return isolation(t)
}

// Isolated is Env for whatever runs git with no test to fail, as a
// TestMain does, or why the git binary is not there.
func Isolated() ([]string, error) {
	bin, err := lookGit()
	if err != nil {
		return nil, err
	}
	return isolated(bin), nil
}

// Runner is lit2's git as a test runs it, under Env and env.
func Runner(t testing.TB, env ...string) *git.Git {
	t.Helper()
	return runner(t, env...)
}

// Physical is a temporary directory by its physical path, as Locate and
// git both report it.
func Physical(t testing.TB) string {
	t.Helper()
	return physical(t)
}

// RepositoryAt is lit's repository at path, it and the directory it is in
// checked from the directory above.
func RepositoryAt(g *git.Git, path string) git.Repository { return repositoryAt(g, path) }

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

// NewRepo is an empty code repository on disk, read as lit2 reads one,
// committed to through the git binary.
func NewRepo(t *testing.T) repo {
	t.Helper()
	return newRepo(t)
}

// Outside is a directory in no checkout, whatever the machine holds above
// the temporary directory, and the environment variable that keeps it
// there: a repository is planted above dir and GIT_CEILING_DIRECTORIES set
// between the two, so git's walk up from dir stops before the planted
// repository, as it stops before any other. A test passes ceilingEnv to
// whatever runs git from dir.
func Outside(t testing.TB) (dir, ceilingEnv string) {
	t.Helper()
	above := Physical(t)
	Git(t, above, "init", "-q")
	between := filepath.Join(above, "ceiling")
	dir = filepath.Join(between, "outside")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, "GIT_CEILING_DIRECTORIES=" + between
}

// git's loose layout, its one spelling among the tests: objects/ fanned
// out by an id's first two hex digits, and refs/ a file per ref.
const (
	looseObjects = "objects"
	looseRefs    = "refs"
)

// LooseObject is the file git keeps id in, loose, in the repository at
// gitDir.
func LooseObject(gitDir string, id git.ObjectID) string {
	s := id.String()
	return filepath.Join(gitDir, looseObjects, s[:2], s[2:])
}

// LooseRef is the file git keeps name in, loose, in the repository at
// gitDir.
func LooseRef(gitDir string, name git.RefName) string {
	return filepath.Join(gitDir, filepath.FromSlash(string(name)))
}

// Stored is what the repository at gitDir holds: its refs as git lists
// them, and the Entries of objects/ and refs/, so two calls differ when
// anything git keeps changed.
func Stored(t testing.TB, gitDir string) string {
	t.Helper()
	held := append(Entries(t, filepath.Join(gitDir, looseObjects)), Entries(t, filepath.Join(gitDir, looseRefs))...)
	return Git(t, gitDir, "for-each-ref") + "\n" + strings.Join(held, "\n")
}

// Entries is every entry under dir, dir itself among them, with its mode,
// size and modification time, so two calls differ when anything there was
// made, removed, written or had its permissions changed.
func Entries(t testing.TB, dir string) []string {
	t.Helper()
	var held []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		held = append(held, fmt.Sprintf("%s %v %d %v", p, info.Mode(), info.Size(), info.ModTime()))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return held
}
