// Package git is the repository lit2 runs in and lit's own: the user's
// git binary writes lit's repository and reads the code repository, and
// go-git reads lit's repository in process.
//
// Its layer in architecture.md §6. Every git process lit2 starts is
// started by Git, which owns the environment git runs under and records
// each process for the command's event. Here are checkout discovery and
// the code repository's object format, both answered by git rev-parse; the
// code repository's HEAD and history, read and never written; and lit's
// own repository's Objects and Refs, a bare repository on disk, and its
// twin in memory.
package git

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/promptctl/lit2/internal/files"
)

// Checkout is one git checkout: its work tree, its private git dir, the
// common dir its clone shares, and its repository's object format. The
// paths are absolute; in a main worktree GitDir and CommonDir are the same
// directory.
type Checkout struct {
	WorkTree, GitDir, CommonDir string
	Format                      ObjectFormat
}

// lit's directory in a git dir, and the private one beneath it.
var (
	litDir      = files.MustName("lit")
	checkoutDir = files.MustName("checkout")
)

// Private is the directory of what lit2 keeps for this checkout alone,
// <git-dir>/lit/checkout, which git deletes with the worktree (D11).
func (c Checkout) Private() files.Dir {
	return files.DirAt(filepath.Join(c.GitDir, litDir.String()), checkoutDir)
}

// Shared is the directory of what lit2 keeps once per clone,
// <git-common-dir>/lit, holding store/, principal and founding, the pin
// naming the enrollment that founded the store (D11, §3.17).
func (c Checkout) Shared() files.Dir { return files.DirAt(c.CommonDir, litDir) }

// NotFound is git finding no repository at or above a directory.
type NotFound struct{ Dir string }

func (n NotFound) Error() string { return fmt.Sprintf("no git checkout at or above %s", n.Dir) }

// Locate is the checkout git finds from dir, as it finds one for the user
// there: under the process's environment, so GIT_DIR, GIT_WORK_TREE and
// GIT_CEILING_DIRECTORIES count as they do for git (D10), in the one git
// process every command starts. git finding no repository is NotFound;
// any other refusal, a bare repository, one git will not trust, or a .git
// naming a git dir that is gone, as a pruned worktree's does, is git's own
// error.
func (g *Git) Locate(dir string) (Checkout, error) {
	out, err := g.run(dir, g.userEnv(), nil, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-dir", "--git-common-dir", "--show-object-format")
	var refused Refused
	// git's words for a search that found nothing, to / or to a ceiling or
	// mount point; a .git it found but cannot follow says "not a git
	// repository: <dir>"
	if errors.As(err, &refused) && strings.Contains(refused.Spawn.Stderr, "not a git repository (or any ") {
		return Checkout{}, NotFound{Dir: dir}
	}
	if err != nil {
		return Checkout{}, err
	}
	lines := strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
	if len(lines) != 4 {
		return Checkout{}, fmt.Errorf("git rev-parse printed %q; want the work tree, git dir, common dir and object format", out)
	}
	c := Checkout{WorkTree: lines[0], GitDir: lines[1], CommonDir: lines[2]}
	for _, f := range []ObjectFormat{SHA1, SHA256} {
		if lines[3] == f.String() {
			c.Format = f
			return c, nil
		}
	}
	return Checkout{}, fmt.Errorf("git names the object format of %s %q, which this lit2 does not read", c.CommonDir, lines[3])
}
