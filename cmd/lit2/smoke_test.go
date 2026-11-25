//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/git/gittest"
)

// lit2 is the binary TestMain built with the documented command, before
// any test ran; Go gives TestMain no channel to a test but a global, so
// newSandbox reads it once and every run takes it from the sandbox.
var lit2 string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "lit2-smoke")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	lit2 = filepath.Join(dir, "lit2")
	goBin, env, err := toolchain()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cmd := exec.Command(goBin, "build", "-o", lit2, ".")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "CGO_ENABLED=0 go build ./cmd/lit2: %v\n%s", err, out)
		os.Exit(1)
	}
	status := m.Run()
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		status = 1
	}
	os.Exit(status)
}

// toolchain is the go command of the toolchain go env reports, and an
// environment for its build made from nothing: where that toolchain, its
// module cache and its build cache are, the git it stamps the module's
// version from, which the binary reads, no go env file, and no cgo, so no
// GOFLAGS, GOWORK or other variable of the caller's shapes the build.
func toolchain() (string, []string, error) {
	out, err := exec.Command("go", "env", "-json", "GOROOT", "GOPATH", "GOMODCACHE", "GOCACHE").Output()
	if err != nil {
		return "", nil, fmt.Errorf("go env: %w", err)
	}
	var at struct{ GOROOT, GOPATH, GOMODCACHE, GOCACHE string }
	if err := json.Unmarshal(out, &at); err != nil {
		return "", nil, fmt.Errorf("go env: %w", err)
	}
	env, err := gittest.Environ()
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(at.GOROOT, "bin", "go"), append(env, "GOROOT="+at.GOROOT, "GOPATH="+at.GOPATH,
		"GOMODCACHE="+at.GOMODCACHE, "GOCACHE="+at.GOCACHE, "GOENV=off", "CGO_ENABLED=0"), nil
}

// sandbox is where a smoke test runs the built lit2: tmp, outside every
// checkout as gittest.NoCheckout makes one, with a HOME and a state
// directory beside it. Every run's environment is derived from tmp and
// built from nothing, so no config, GIT_DIR or HOME of the caller's
// reaches a run.
type sandbox struct{ lit2, tmp string }

func newSandbox(t *testing.T) sandbox {
	t.Helper()
	tmp, _ := gittest.NoCheckout(t)
	return sandbox{lit2: lit2, tmp: tmp}
}

// home is lit2's HOME in sb.
func (sb sandbox) home() string { return filepath.Join(filepath.Dir(sb.tmp), "home") }

// state is lit2's XDG_STATE_HOME in sb, where it keeps its command log.
func (sb sandbox) state() string { return filepath.Join(filepath.Dir(sb.tmp), "state") }

// command is lit2 with args in dir, in sb: under the git a test's lit2
// runs, stopped at the ceiling above tmp, with sb's HOME and state.
func (sb sandbox) command(t *testing.T, dir string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(sb.lit2, args...)
	cmd.Dir, cmd.Env = dir, append(gittest.Env(t), gittest.Ceiling(sb.tmp), "HOME="+sb.home(), "XDG_STATE_HOME="+sb.state())
	return cmd
}

// TestSmoke runs the built lit2 as a person would, outside any checkout and
// inside two not yet minted, whose workspace drafts are not in force, one
// that does not parse and one that does, with a GIT_DIR and a HOME of its
// own around it that no run may see.
func TestSmoke(t *testing.T) {
	sb := newSandbox(t)
	tmp, state := sb.tmp, sb.state()
	hostile := filepath.Join(filepath.Dir(tmp), "hostile")
	gittest.Git(t, filepath.Dir(tmp), "init", "-q", hostile)
	if err := os.WriteFile(filepath.Join(hostile, ".gitconfig"), []byte("[init]\n\tdefaultBranch = hostile\n[alias]\n\tinit = status\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_DIR", filepath.Join(hostile, ".git"))
	t.Setenv("HOME", hostile)
	broken := checkout(t, filepath.Join(tmp, "broken"), "prefix = [unclosed\n")
	parses := checkout(t, filepath.Join(tmp, "parses"), oneLevel)

	var statuses []int
	for _, tc := range []struct {
		args         []string
		status       int
		stdout, errs string
		brokenPipe   bool
		dir          string
	}{
		{[]string{"--help"}, 0, "admin sync", "", false, tmp},
		{[]string{"--help"}, 0, "admin init", "", false, tmp},
		{nil, 78, "", "error (reason=store): the store: no git checkout at or above " + tmp + "\n", false, tmp},
		{[]string{"--help"}, 0, "", "", true, tmp},
		{[]string{"--help"}, 0, "admin sync", "", false, broken},
		{[]string{"--help"}, 0, "admin sync", "", false, parses},
	} {
		cmd := sb.command(t, tc.dir, tc.args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if tc.brokenPipe {
			cmd.Stdout = brokenPipe(t)
		}
		status := 0
		var exit *exec.ExitError
		if err := cmd.Run(); errors.As(err, &exit) {
			status = exit.ExitCode()
		} else if err != nil {
			t.Fatalf("lit2 %q did not run: %v", tc.args, err)
		}
		statuses = append(statuses, status)
		for _, verb := range []string{"get", "set", "add", "rm"} {
			if tc.stdout != "" && !strings.Contains(stdout.String(), verb) {
				t.Errorf("lit2 %q does not name %s on stdout:\n%s", tc.args, verb, stdout.String())
			}
		}
		if status != tc.status || !holds(stdout.String(), tc.stdout) || !strings.HasPrefix(stderr.String(), tc.errs) || (tc.errs == "") != (stderr.Len() == 0) {
			t.Errorf("lit2 %q: status %d, stdout %q, stderr %q", tc.args, status, stdout.String(), stderr.String())
		}
	}

	var recorded []int
	var configs []*configLine
	days, err := filepath.Glob(filepath.Join(state, "lit", "commands", "*.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, day := range days {
		b, err := os.ReadFile(day)
		if err != nil {
			t.Fatal(err)
		}
		for l := range bytes.Lines(b) {
			var line struct {
				ExitCode int         `json:"exit_code"`
				Outcome  string      `json:"outcome"`
				Config   *configLine `json:"config"`
			}
			if err := json.Unmarshal(l, &line); err != nil || (line.ExitCode == 0) != (line.Outcome == "ok") {
				t.Errorf("log line %q does not decode to an exit code and its outcome: %v", l, err)
			}
			recorded = append(recorded, line.ExitCode)
			configs = append(configs, line.Config)
		}
	}
	if len(recorded) != len(statuses) {
		t.Fatalf("the command log holds %d lines for %d runs", len(recorded), len(statuses))
	}
	for i := range statuses {
		if recorded[i] != statuses[i] {
			t.Errorf("run %d exited %d and its line records %d", i, statuses[i], recorded[i])
		}
	}
	// No run read a version: only an applied one is in force, never the
	// draft beside it, and nothing stands in for none (architecture.md §3.15).
	for i, c := range configs {
		if c != nil {
			t.Errorf("run %d records config %+v; want none, no version being applied", i, *c)
		}
	}
}

// holds is whether stdout is what a row expects of it: a row's text appears
// in it, and a row with none expects nothing written, since every string
// contains the empty one.
func holds(stdout, want string) bool {
	if want == "" {
		return stdout == ""
	}
	return strings.Contains(stdout, want)
}

// TestSmokeWorksATicket makes a checkout a writer, the default profile its
// version one, and works one ticket in it by path, through the built
// binary and the store on disk.
func TestSmokeWorksATicket(t *testing.T) {
	sb := newSandbox(t)
	dir := checkout(t, filepath.Join(sb.tmp, "work"), "")
	lit := func(args ...string) string { t.Helper(); return run(t, sb, dir, 0, args...) }
	if out := lit("admin", "init"); !strings.HasPrefix(out, "principal did:key:") || !strings.Contains(out, "recovery phrase: ") || !strings.Contains(out, "enrolled: policy v1, owner did:key:") || !strings.Contains(out, "applied as version one") {
		t.Fatalf("admin init printed %q", out)
	}
	first, _, _ := strings.Cut(lit("add", "unfiled/children", "{title: a}"), "\n")
	id := strings.TrimPrefix(first, "+ ")
	if got := lit("get", id+"/title"); got != "a\n" {
		t.Errorf("get title = %q", got)
	}
	lit("set", id+"/title", "b")
	lit("add", id+"/comments", "a note")
	lit("add", id+"/labels", "ready")
	if got := lit("get", id+"/title", id+"/labels", id+"/ready"); got != id+"/title\tb\n"+id+"/labels\tready\n"+id+"/ready\tunmet description (ready)\n" {
		t.Errorf("get title, labels and ready = %q", got)
	}
	lit("rm", id)
	lit("set", id+"/deleted", "false")
	if got := lit("get", id+"/deleted"); got != "false\n" {
		t.Errorf("a restored item's deleted = %q", got)
	}
	if got := lit(); !strings.Contains(got, "unfiled/children\t"+id+" · initial · b\n") || strings.Contains(got, "$/lanes") {
		t.Errorf("a bare lit2 does not show the feature in the tree and no lanes:\n%s", got)
	}
}

// TestSmokeAppliesTheWorkspace walks the ticket's done-when through real
// git: init leaves a file whose blob is the version in force; an edit
// leaves the old model in force with one line on the board, and is refused
// until committed; once committed and applied the next command runs the
// new model; a file that does not parse stops no command.
func TestSmokeAppliesTheWorkspace(t *testing.T) {
	sb := newSandbox(t)
	dir := checkout(t, filepath.Join(sb.tmp, "work"), "")
	lit := func(status int, args ...string) string { t.Helper(); return run(t, sb, dir, status, args...) }
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, ".lit", "workspace.toml"), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lit(0, "admin", "init")
	first := gittest.Git(t, dir, "hash-object", ".lit/workspace.toml")
	if got := lit(0, "get", "/workspace"); !strings.Contains(got, first[:7]+" in force · .lit/workspace.toml matches") {
		t.Errorf("the board after init: %q; want %s in force", got, first[:7])
	}
	if got := lit(0, "config", "status"); !strings.Contains(got, "in HEAD's history: no") {
		t.Errorf("status before the file is committed:\n%s", got)
	}
	gittest.Git(t, dir, "add", ".lit/workspace.toml")
	gittest.Git(t, dir, "commit", "-q", "-m", "workspace")
	if got := lit(0, "config", "status"); !strings.Contains(got, "in HEAD's history: yes") {
		t.Errorf("status once the file is committed:\n%s", got)
	}
	if got := lit(0, "config", "commit"); !strings.Contains(got, "is already in force") {
		t.Errorf("committing the version in force: %q", got)
	}
	write(withIcebox)
	if got := lit(0); !strings.Contains(got, first[:7]+" in force · .lit/workspace.toml differs (") || !strings.Contains(got, "unassigned · home") {
		t.Errorf("the board beside an edit, the default still in force:\n%s", got)
	}
	lit(1, "config", "commit")
	gittest.Git(t, dir, "commit", "-q", "-am", "icebox")
	blob := gittest.Git(t, dir, "rev-parse", "HEAD:.lit/workspace.toml")
	if got := lit(0, "config", "commit"); got != "applied "+blob+"\n" {
		t.Errorf("config commit printed %q; want applied %s", got, blob)
	}
	feature, _, _ := strings.Cut(lit(0, "add", "unfiled/children", "{title: f}"), "\n")
	story, _, _ := strings.Cut(lit(0, "add", strings.TrimPrefix(feature, "+ ")+"/children", "{title: s}"), "\n")
	story = strings.TrimPrefix(story, "+ ")
	lit(0, "set", story+"/status", "icebox")
	if got := lit(0, "get", story+"/status"); got != "icebox\n" {
		t.Errorf("a story moved into the new column reads %q", got)
	}
	write("prefix = [unclosed\n")
	if got := lit(0); !strings.Contains(got, blob[:7]+" in force · .lit/workspace.toml differs (") {
		t.Errorf("the board beside a file that does not parse:\n%s", got)
	}
}

// withIcebox is a model that is not the default: two levels, and an icebox
// column a story moves into from the backlog.
const withIcebox = `prefix = "lit"
claim_ttl = "6h"

[levels.1]
name = "story"

[levels.2]
name = "feature"
home = "unfiled"

[columns.backlog]
initial = true
next = ["done", "icebox"]

[columns.icebox]
terminal = true

[columns.done]
terminal = true
`

// run runs lit2 in dir, in sb, and is its stdout, failing t unless it
// exits status.
func run(t *testing.T, sb sandbox, dir string, status int, args ...string) string {
	t.Helper()
	cmd := sb.command(t, dir, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	got := 0
	var exit *exec.ExitError
	if err := cmd.Run(); errors.As(err, &exit) {
		got = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("lit2 %q did not run: %v", args, err)
	}
	if got != status {
		t.Fatalf("lit2 %q exited %d, stderr %q; want %d", args, got, stderr.String(), status)
	}
	return stdout.String()
}

// configLine is the command log's config block, as a reader of the log sees it.
type configLine struct {
	Ref string `json:"ref"`
}

// oneLevel is a draft that parses and is not the default: one level where the
// default has three.
const oneLevel = `prefix = "lit"
claim_ttl = "6h"

[levels.1]
name = "story"

[columns.backlog]
initial = true
next = ["done"]

[columns.done]
terminal = true
`

// checkout makes a git checkout at dir whose .lit/workspace.toml is
// draft, or with none when draft is empty, and returns dir.
func checkout(t *testing.T, dir, draft string) string {
	t.Helper()
	gittest.Git(t, filepath.Dir(dir), "init", "--quiet", dir)
	if draft == "" {
		return dir
	}
	if err := os.MkdirAll(filepath.Join(dir, ".lit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".lit", "workspace.toml"), []byte(draft), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// brokenPipe is the write end of a pipe whose reader closed before lit2
// started, so lit2's first write to it gets EPIPE, with no timing involved.
func brokenPipe(t *testing.T) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := w.Close(); err != nil {
			t.Error(err)
		}
	})
	return w
}
