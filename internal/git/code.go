package git

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// Code is the code repository as lit2 reads it, never writes it: the blob
// HEAD holds at a path, and whether a blob was ever at a path in HEAD's
// history. A path is slash-separated, from the work tree's root.
type Code interface {
	AtHead(path string) (HeadBlob, error)
	EverAt(path string, id ObjectID) (bool, error)
}

// HeadBlob is what HEAD's tree holds at a path: a file's blob, something
// that is not a file, or nothing.
//
//sumtype:decl
type HeadBlob interface{ headBlob() }

// Tracked is a blob HEAD's tree holds at the path.
type Tracked struct{ Blob ObjectID }

// NotAFile is an entry at the path in HEAD's tree that holds no file: a
// directory, a symlink, whose blob is its target's name, or a submodule,
// by its git mode.
type NotAFile struct{ Mode string }

// Untracked is no entry at the path in HEAD's tree, or a HEAD with no commit.
type Untracked struct{}

func (Tracked) headBlob()   {}
func (NotAFile) headBlob()  {}
func (Untracked) headBlob() {}

// CodeAt is the code repository of the checkout c, in its object format,
// read by git: HEAD through the checkout's own git dir, so a linked
// worktree reads its own branch, under the process's environment, as git
// reads it for the user. Every path is literal, from the work tree's root.
func (g *Git) CodeAt(c Checkout) Code { return codeRepo{g: g, c: c} }

type codeRepo struct {
	g *Git
	c Checkout
}

func (r codeRepo) git(stdin []byte, args ...string) (string, error) {
	out, err := r.g.run("", r.g.userEnv(), stdin, append([]string{"--git-dir=" + r.c.GitDir, "--literal-pathspecs"}, args...)...)
	return string(out), err
}

// head is HEAD's commit, or false when HEAD names a branch with no commit.
// git rev-parse --verify answers exit 1, saying nothing, both for that
// branch and for a HEAD whose object is missing, so only then is the
// branch itself asked whether it names anything: a damaged repository is
// git's error, never a branch with no commit `[LAW:no-silent-failure]`.
func (r codeRepo) head() (string, bool, error) {
	out, err := r.git(nil, "rev-parse", "-q", "--verify", "HEAD^{commit}")
	if code, ok := exitCode(err); !ok || code != 1 {
		return strings.TrimSuffix(out, "\n"), err == nil, err
	}
	branch, err := r.git(nil, "symbolic-ref", "-q", "HEAD")
	if err != nil {
		return "", false, fmt.Errorf("HEAD names no commit: %w", err)
	}
	names, err := r.git(nil, "for-each-ref", "--format=%(objectname)", "--", strings.TrimSuffix(branch, "\n"))
	if err == nil && names != "" {
		err = fmt.Errorf("HEAD's %s names %s, which is %w", strings.TrimSpace(branch), strings.TrimSpace(names), errHeadMissing)
	}
	return "", false, err
}

// errHeadMissing is a HEAD whose branch names an object the repository
// does not hold; errShallow is a question a clone cut short at its
// boundary cannot answer.
var (
	errHeadMissing = errors.New("no commit this repository holds")
	errShallow     = errors.New("unknown: HEAD's history is not all in this clone, as in a shallow one")
)

// AtHead reads HEAD's entry at path. A file is a blob of a regular mode;
// any other entry is NotAFile.
func (r codeRepo) AtHead(path string) (HeadBlob, error) {
	commit, ok, err := r.head()
	if err != nil || !ok {
		return Untracked{}, err
	}
	out, err := r.git(nil, "ls-tree", "-z", "--full-tree", commit, "--", path)
	if err != nil {
		return nil, err
	}
	for _, e := range strings.Split(out, "\x00") {
		meta, p, _ := strings.Cut(e, "\t")
		f := strings.Fields(meta)
		switch {
		case p != path || len(f) != 3:
		case regular(f[0]):
			id, err := ParseObjectID(r.c.Format, f[2])
			return Tracked{Blob: id}, err
		default:
			return NotAFile{Mode: f[0]}, nil
		}
	}
	return Untracked{}, nil
}

// regular is a mode of a file in the code repository: lit's own, or
// executable.
func regular(mode string) bool { return mode == fileMode || mode == "100755" }

// EverAt asks whether any commit in HEAD's history holds id at path. Every
// blob ever at a path is one some commit's diff against a parent, or a
// root's against nothing, puts there, so the diffs of every commit that
// touches it, merges against each parent, answer it whole. A blob not
// found in a shallow clone may be past its boundary, so the answer is
// unknown, refused as such, never answered no.
func (r codeRepo) EverAt(path string, id ObjectID) (bool, error) {
	if err := inFormat(id, r.c.Format); err != nil {
		return false, err
	}
	commit, ok, err := r.head()
	if err != nil || !ok {
		return false, err
	}
	commits, err := r.git(nil, "rev-list", "--full-history", commit, "--", path)
	if err != nil {
		return false, err
	}
	out, err := r.git([]byte(commits), "diff-tree", "--stdin", "--no-commit-id", "-m", "-r", "--root", "--no-renames", "-z", "--", path)
	if err != nil {
		return false, err
	}
	// each change is ":<mode> <mode> <id> <id> <status>", a NUL, its path, a NUL
	fields := strings.Split(out, "\x00")
	for i := 0; i+1 < len(fields); i += 2 {
		f := strings.Fields(strings.TrimPrefix(fields[i], ":"))
		if len(f) == 5 && fields[i+1] == path && regular(f[1]) && f[3] == id.String() {
			return true, nil
		}
	}
	shallow, err := r.git(nil, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(shallow) == "true" {
		return false, fmt.Errorf("whether %s was ever %s is %w", id, path, errShallow)
	}
	return false, nil
}

// CodeMemory is the code repository in process, SHA-1, its history a list
// of commits, each the whole tree as path to blob. It is Code's twin, held
// to the same contract suite.
type CodeMemory struct {
	mu      sync.Mutex
	commits []map[string]ObjectID
}

// Commit makes files, path to bytes, the tree of a new commit on HEAD; a
// path not in files is not in the tree.
func (m *CodeMemory) Commit(files map[string][]byte) {
	tree := make(map[string]ObjectID, len(files))
	for p, b := range files {
		tree[p] = HashBlob(SHA1, b)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.commits = append(m.commits, tree)
}

func (m *CodeMemory) AtHead(path string) (HeadBlob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.commits) == 0 {
		return Untracked{}, nil
	}
	if id, ok := m.commits[len(m.commits)-1][path]; ok {
		return Tracked{Blob: id}, nil
	}
	return Untracked{}, nil
}

func (m *CodeMemory) EverAt(path string, id ObjectID) (bool, error) {
	if err := inFormat(id, SHA1); err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.commits {
		if at, ok := c[path]; ok && at == id {
			return true, nil
		}
	}
	return false, nil
}
