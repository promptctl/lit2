// Package git is the repository lit2 runs in.
//
// Its layer in architecture.md §6: go-git, in process, for objects and refs;
// the user's git binary for push and fetch, run only by the background sync
// process (decisions B and G). Here it holds checkout discovery: the work
// tree, the checkout's private git dir and the common dir every worktree of
// a clone shares, read in process from git's own layout (D10), with no git
// binary and no GIT_DIR.
package git

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Checkout is one git checkout: its work tree, its private git dir and the
// common dir its clone shares. The paths are absolute; in a main worktree
// GitDir and CommonDir are the same directory.
type Checkout struct{ WorkTree, GitDir, CommonDir string }

// PrivateDir is the root of what lit2 keeps for this checkout alone,
// <git-dir>/lit/checkout, which git deletes with the worktree (D11).
func (c Checkout) PrivateDir() string { return filepath.Join(c.GitDir, "lit", "checkout") }

// SharedDir is the root of what lit2 keeps once per clone,
// <git-common-dir>/lit, holding store/ and principal (D11).
func (c Checkout) SharedDir() string { return filepath.Join(c.CommonDir, "lit") }

// NotFound is the walk reaching the filesystem root with no .git on the way.
type NotFound struct{ Dir string }

func (n NotFound) Error() string { return fmt.Sprintf("no git checkout at or above %s", n.Dir) }

// Locate walks up from dir, an absolute directory, to the nearest .git: a
// directory is the git dir, a file names it by its gitdir: line. The git
// dir's commondir file, when present, names the common dir. It returns
// NotFound when no .git is found, or the error that stopped the walk.
func Locate(dir string) (Checkout, error) {
	for at := dir; ; at = filepath.Dir(at) {
		dotgit := filepath.Join(at, ".git")
		info, err := os.Stat(dotgit)
		if errors.Is(err, fs.ErrNotExist) {
			if filepath.Dir(at) == at {
				return Checkout{}, NotFound{Dir: dir}
			}
			continue
		}
		if err != nil {
			return Checkout{}, err
		}
		gitDir := dotgit
		if !info.IsDir() {
			if gitDir, err = readGitdirFile(dotgit); err != nil {
				return Checkout{}, err
			}
		}
		// as git's own discovery does, cheaply: a git dir holds HEAD
		head, err := os.Stat(filepath.Join(gitDir, "HEAD"))
		if err != nil {
			return Checkout{}, fmt.Errorf("%s is not a git dir: %w", gitDir, err)
		}
		if !head.Mode().IsRegular() {
			return Checkout{}, fmt.Errorf("%s is not a git dir: its HEAD is not a file", gitDir)
		}
		commonDir, err := readCommondir(gitDir)
		if err != nil {
			return Checkout{}, err
		}
		return Checkout{WorkTree: at, GitDir: gitDir, CommonDir: commonDir}, nil
	}
}

// readGitdirFile reads a .git file, as git worktree add and submodules write
// it: one gitdir: line naming the git dir, relative to the file's directory
// or absolute. The git dir it names must be a directory: a pruned or copied
// worktree's .git file names one that is gone, and that is no checkout
// `[LAW:parse-dont-validate]`.
func readGitdirFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	named, ok := strings.CutPrefix(strings.TrimRight(string(b), "\r\n"), "gitdir: ")
	if !ok {
		return "", fmt.Errorf("%s: no gitdir: line", path)
	}
	return namedDir(path, filepath.Dir(path), named)
}

// readCommondir reads <git-dir>/commondir, which a linked worktree's git dir
// holds and a main worktree's does not; with none, the git dir is the common
// dir.
func readCommondir(gitDir string) (string, error) {
	path := filepath.Join(gitDir, "commondir")
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return gitDir, nil
	}
	if err != nil {
		return "", err
	}
	return namedDir(path, gitDir, strings.TrimRight(string(b), "\r\n"))
}

// namedDir is the directory the file at path names: named, absolute or
// relative to base, non-empty and a directory, or an error naming path.
func namedDir(path, base, named string) (string, error) {
	if named == "" {
		return "", fmt.Errorf("%s names no directory", path)
	}
	dir := resolve(base, named)
	info, err := os.Stat(dir)
	if err != nil {
		return "", fmt.Errorf("%s names a directory that cannot be read: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s names %s, which is not a directory", path, dir)
	}
	return dir, nil
}

// resolve reads a path git wrote, which is absolute or relative to base.
func resolve(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}
