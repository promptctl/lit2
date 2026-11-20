package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
)

// Git is the user's git binary, the one writer of lit's repository and
// the one reader of the code repository. Every git process
// lit2 starts is started here, so this is where the environment git runs
// under is decided and where each process is recorded
// `[LAW:single-enforcer]` `[LAW:nothing-unseen]`.
type Git struct {
	bin string
	// missing is why there is no git: every spawn fails with it, so a git
	// missing from PATH is one loud error, never a fallback
	// `[LAW:no-silent-failure]`.
	missing error
	// env is the environment the process was given, which the user's own
	// repositories are read under, as git reads them there.
	env []string
	log *spawnLog
}

// Spawn is one git process as the command's event records it: its
// arguments, its exit status, -1 when it never ran, and its stderr,
// scrubbed.
type Spawn struct {
	Args   []string
	Exit   int
	Stderr string
}

type spawnLog struct {
	mu   sync.Mutex
	done []Spawn
}

// Find is git as the process environment env names it: the first
// executable git on its PATH.
func Find(env []string) Git {
	g := Git{env: env, log: &spawnLog{done: []Spawn{}}}
	for _, kv := range env {
		path, ok := strings.CutPrefix(kv, "PATH=")
		if !ok {
			continue
		}
		for _, dir := range filepath.SplitList(path) {
			if bin, err := exec.LookPath(filepath.Join(dir, "git")); err == nil && filepath.IsAbs(bin) {
				g.bin = bin
				return g
			}
		}
	}
	g.missing = errors.New("git is not on PATH: lit2 writes its repository and reads the code repository through the user's git")
	return g
}

// Spawns is the record of every git process this Git ran, in the order
// each ended.
func (g Git) Spawns() []Spawn {
	g.log.mu.Lock()
	defer g.log.mu.Unlock()
	return slices.Clone(g.log.done)
}

// Refused is a git process that failed: the spawn says what git said.
type Refused struct {
	Spawn Spawn
	Err   error
}

func (r Refused) Error() string {
	return fmt.Sprintf("git %s: %v: %s", strings.Join(r.Spawn.Args, " "), r.Err, r.Spawn.Stderr)
}

func (r Refused) Unwrap() error { return r.Err }

// userEnv is the environment the user's own repositories are read under:
// the process's, so git finds and reads them as it does for the user, and
// messages in the C locale, so a refusal reads the same everywhere.
func (g Git) userEnv() []string { return append(slices.Clone(g.env), "LC_ALL=C") }

// litEnv is the environment lit's repository is written under: no GIT_
// variable of the caller's, which a hook sets to redirect objects into
// the user's repository, no user or system config, which could change an
// object's bytes, and lit's fixed identity at time zero, so a commit is a
// pure function of its tree and message `[LAW:effects-at-boundaries]`.
func (g Git) litEnv() []string {
	env := slices.DeleteFunc(slices.Clone(g.env), func(kv string) bool { return strings.HasPrefix(kv, "GIT_") })
	return append(env, "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME=lit", "GIT_AUTHOR_EMAIL=lit", "GIT_AUTHOR_DATE=@0 +0000",
		"GIT_COMMITTER_NAME=lit", "GIT_COMMITTER_EMAIL=lit", "GIT_COMMITTER_DATE=@0 +0000")
}

// run runs git with args in dir, "" for the process's own, under env with
// stdin, and is its stdout, or Refused.
func (g Git) run(dir string, env []string, stdin []byte, args ...string) ([]byte, error) {
	if g.missing != nil {
		return nil, g.missing
	}
	cmd := exec.Command(g.bin, args...)
	cmd.Dir, cmd.Env, cmd.Stdin = dir, env, bytes.NewReader(stdin)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	// ProcessState says -1 for a git that never ran
	s := Spawn{Args: slices.Clone(args), Exit: cmd.ProcessState.ExitCode(), Stderr: scrub(stderr.Bytes())}
	g.log.mu.Lock()
	g.log.done = append(g.log.done, s)
	g.log.mu.Unlock()
	if err != nil {
		return nil, Refused{Spawn: s, Err: err}
	}
	return out.Bytes(), nil
}

// stderrCap bounds what one spawn's stderr adds to the event; git's own
// verdict, its last line, is kept.
const stderrCap = 4096

var credentials = regexp.MustCompile(`(\w+://)[^/@\s]+@`)

// scrub is stderr as the event may carry it: its last stderrCap bytes,
// with any credentials in a URL replaced, since a transport error prints
// the URL it was given.
func scrub(b []byte) string {
	s := strings.TrimRight(string(b), "\n")
	if len(s) > stderrCap {
		s = "…" + s[len(s)-stderrCap:]
	}
	return credentials.ReplaceAllString(s, "${1}***@")
}
