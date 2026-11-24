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
	mu  sync.Mutex
	log []Spawn
}

// Spawn is one git process as the command's event records it: its
// arguments, how it ended, and its stderr, scrubbed.
type Spawn struct {
	Args   []string
	Ended  Ended
	Stderr string
}

// Ended is how a spawn ended: git exited with a status, or never started.
//
//sumtype:decl
type Ended interface{ ended() }

// Exited is git run to its end, with its exit status.
type Exited struct{ Code int }

// Unstarted is a git that never ran, and why.
type Unstarted struct{ Why string }

func (Exited) ended()    {}
func (Unstarted) ended() {}

// Find is git as the process environment env names it: the first
// executable git in an absolute directory on its PATH. A relative entry,
// the empty one among them, names a directory only relative to wherever
// lit2 happens to run, so it names no git, as Go's os/exec refuses one.
func Find(env []string) *Git {
	g := &Git{env: env, log: []Spawn{}}
	for _, kv := range env {
		path, ok := strings.CutPrefix(kv, "PATH=")
		if !ok {
			continue
		}
		for _, dir := range filepath.SplitList(path) {
			if !filepath.IsAbs(dir) {
				continue
			}
			if bin, err := exec.LookPath(filepath.Join(dir, "git")); err == nil {
				g.bin = bin
				return g
			}
		}
	}
	g.missing = errors.New("git is not in an absolute directory on PATH: lit2 writes its repository and reads the code repository through the user's git")
	return g
}

// Spawns is the record of every git process this Git ran or tried to, in
// the order each ended.
func (g *Git) Spawns() []Spawn {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.log)
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
func (g *Git) userEnv() []string { return append(slices.Clone(g.env), "LC_ALL=C") }

// litEnv is the environment lit's repository is written under: no GIT_
// variable of the caller's, which a hook sets to redirect objects into
// the user's repository, and no user or system config, which could change
// how git stores what lit hands it.
func (g *Git) litEnv() []string {
	env := slices.DeleteFunc(slices.Clone(g.env), func(kv string) bool { return strings.HasPrefix(kv, "GIT_") })
	return append(env, "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
}

// run runs git with args in dir, "" for the process's own, under env with
// stdin, and is its stdout, or Refused. Every attempt is recorded, one
// that never started included.
func (g *Git) run(dir string, env []string, stdin []byte, args ...string) ([]byte, error) {
	s := Spawn{Args: slices.Clone(args)}
	out, err := g.exec(dir, env, stdin, &s)
	g.mu.Lock()
	g.log = append(g.log, s)
	g.mu.Unlock()
	if err != nil {
		return nil, Refused{Spawn: s, Err: err}
	}
	return out, nil
}

// exec runs one git process and fills in how s ended.
func (g *Git) exec(dir string, env []string, stdin []byte, s *Spawn) ([]byte, error) {
	if g.missing != nil {
		s.Ended = Unstarted{Why: g.missing.Error()}
		return nil, g.missing
	}
	cmd := exec.Command(g.bin, s.Args...)
	cmd.Dir, cmd.Env, cmd.Stdin = dir, env, bytes.NewReader(stdin)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	s.Stderr = scrub(stderr.Bytes())
	if cmd.ProcessState == nil {
		s.Ended = Unstarted{Why: err.Error()}
		return nil, err
	}
	s.Ended = Exited{Code: cmd.ProcessState.ExitCode()}
	return out.Bytes(), err
}

// exitCode is the status git exited with, or false for a git that never
// ran.
func exitCode(err error) (int, bool) {
	var refused Refused
	if !errors.As(err, &refused) {
		return 0, false
	}
	e, ok := refused.Spawn.Ended.(Exited)
	return e.Code, ok
}

// stderrCap bounds what one spawn's stderr adds to the event; git's own
// verdict, its last line, is kept.
const stderrCap = 4096

// credentials is a URL's userinfo: everything from its scheme to the last
// @ before its host, so a password holding an @ is replaced whole.
var credentials = regexp.MustCompile(`(\w+://)[^/\s]*@`)

// scrub is stderr as the event may carry it: any credentials in a URL
// replaced, since a transport error prints the URL it was given, and then
// its last stderrCap bytes, so a cut can never split a URL from its
// scheme before the credentials are gone.
func scrub(b []byte) string {
	s := credentials.ReplaceAllString(strings.TrimRight(string(b), "\n"), "${1}***@")
	if len(s) > stderrCap {
		s = "…" + s[len(s)-stderrCap:]
	}
	return s
}
