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

// HeadBlob is what HEAD's tree holds at a path: a blob, or none.
//
//sumtype:decl
type HeadBlob interface{ headBlob() }

// Tracked is a blob HEAD's tree holds at the path.
type Tracked struct{ Blob ObjectID }

// Untracked is no blob at the path in HEAD's tree, or a HEAD with no commit.
type Untracked struct{}

func (Tracked) headBlob()   {}
func (Untracked) headBlob() {}

// CodeAt is the code repository of the checkout c, in format f, read by
// git: HEAD through the checkout's own git dir, so a linked worktree reads
// its own branch, under the process's environment, as git reads it for
// the user. Every path is literal, from the work tree's root.
func (g Git) CodeAt(c Checkout, f ObjectFormat) Code { return codeRepo{g: g, c: c, f: f} }

type codeRepo struct {
	g Git
	c Checkout
	f ObjectFormat
}

func (r codeRepo) git(stdin []byte, args ...string) (string, error) {
	out, err := r.g.run("", r.g.userEnv(), stdin, append([]string{"--git-dir=" + r.c.GitDir, "--literal-pathspecs"}, args...)...)
	return string(out), err
}

// head is HEAD's commit, or false when HEAD names a branch with no commit,
// which git rev-parse --verify answers with exit 1 and nothing said.
func (r codeRepo) head() (string, bool, error) {
	out, err := r.git(nil, "rev-parse", "-q", "--verify", "HEAD^{commit}")
	var refused Refused
	if errors.As(err, &refused) && refused.Spawn.Exit == 1 {
		return "", false, nil
	}
	return strings.TrimSuffix(out, "\n"), err == nil, err
}

// AtHead reads HEAD's entry at path. A file is a blob of a regular mode;
// a directory, a symlink, whose blob is its target's name, or a submodule
// holds no file there.
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
		if p == path && len(f) == 3 && regular(f[0]) {
			id, err := ParseObjectID(r.f, f[2])
			return Tracked{Blob: id}, err
		}
	}
	return Untracked{}, nil
}

func regular(mode string) bool { return mode == "100644" || mode == "100755" }

// EverAt asks whether any commit in HEAD's history holds id at path. Every
// blob ever at a path is one some commit's diff against a parent, or a
// root's against nothing, puts there, so the diffs of every commit that
// touches it, merges against each parent, answer it whole. A blob not
// found in a shallow clone may be past its boundary, so the answer is
// unknown, refused as such, never answered no.
func (r codeRepo) EverAt(path string, id ObjectID) (bool, error) {
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
		return false, fmt.Errorf("HEAD's history is not all in this clone, as in a shallow one, so whether %s was ever %s is unknown", id, path)
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
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.commits {
		if at, ok := c[path]; ok && at == id {
			return true, nil
		}
	}
	return false, nil
}
