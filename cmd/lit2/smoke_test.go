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

// lit2 is the binary TestMain built, or why it did not; built is its one
// reader.
var lit2 struct {
	bin string
	err error
}

// built is the binary TestMain built, failing t when it did not.
func built(t *testing.T) string {
	t.Helper()
	if lit2.err != nil {
		t.Fatal(lit2.err)
	}
	return lit2.bin
}

// TestMain builds lit2 once for every smoke test, with the documented
// command, and removes it after.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "lit2-smoke")
	if err != nil {
		lit2.err = err
	} else {
		lit2.bin, lit2.err = build(filepath.Join(dir, "lit2"))
	}
	status := m.Run()
	if err == nil {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			status = 1
		}
	}
	os.Exit(status)
}

// toolchain is what go env names of the developer's toolchain, the one
// thing the smoke build takes from the machine.
var toolchain = []string{"GOROOT", "GOPATH", "GOMODCACHE", "GOCACHE"}

// build builds lit2 to bin with the documented command, CGO_ENABLED=0 go
// build, by the go under the GOROOT go env reports, in an environment made
// from nothing but that toolchain's directories and git on PATH: no config
// file, workspace, GOFLAGS or toolchain switch of the developer's reaches
// it, and git is there for the version stamp lit2 refuses to run without.
func build(bin string) (string, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return "", fmt.Errorf("the smoke build stamps lit2's version from git: %w", err)
	}
	out, err := exec.Command("go", append([]string{"env", "-json"}, toolchain...)...).Output()
	if err != nil {
		return "", fmt.Errorf("go env: %w", err)
	}
	var reported map[string]string
	if err := json.Unmarshal(out, &reported); err != nil {
		return "", fmt.Errorf("go env -json: %w", err)
	}
	env := append(gittest.Isolated(git), "GOENV=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off", "CGO_ENABLED=0")
	for _, k := range toolchain {
		if reported[k] == "" {
			return "", fmt.Errorf("go env reports no %s", k)
		}
		env = append(env, k+"="+reported[k])
	}
	cmd := exec.Command(filepath.Join(reported["GOROOT"], "bin", "go"), "build", "-o", bin, ".")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("CGO_ENABLED=0 go build ./cmd/lit2: %w\n%s", err, out)
	}
	return bin, nil
}

// sandbox is where a smoke test's runs live: a directory in no checkout,
// whatever the machine holds above it, with the runs' home and state under
// it. Every run's environment is made from it, never inherited.
type sandbox struct {
	dir     string
	ceiling string // the variable that keeps dir in no checkout
}

// newSandbox is a fresh sandbox. The test's own GIT_DIR and HOME name a
// place no run may read, so a run that inherited them would fail.
func newSandbox(t *testing.T) sandbox {
	t.Helper()
	dir, ceiling := gittest.Outside(t)
	sb := sandbox{dir: dir, ceiling: ceiling}
	t.Setenv("GIT_DIR", filepath.Join(dir, "not-a-repository"))
	t.Setenv("HOME", filepath.Join(dir, "not-a-home"))
	if err := os.Mkdir(sb.home(), 0o755); err != nil {
		t.Fatal(err)
	}
	return sb
}

func (sb sandbox) home() string  { return filepath.Join(sb.dir, "home") }
func (sb sandbox) state() string { return filepath.Join(sb.dir, "state") }

// env is a run's whole environment: the sandbox's home and state, the
// ceiling, and the git the oracle runs.
func (sb sandbox) env(t *testing.T) []string {
	t.Helper()
	return append([]string{"HOME=" + sb.home(), "XDG_STATE_HOME=" + sb.state(), sb.ceiling}, gittest.Env(t)...)
}

// run runs the built lit2 in dir and is its status and what it wrote,
// failing t when it does not run.
func (sb sandbox) run(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(built(t), args...)
	cmd.Dir, cmd.Env = dir, sb.env(t)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	var exit *exec.ExitError
	switch err := cmd.Run(); {
	case errors.As(err, &exit):
		return exit.ExitCode(), stdout.String(), stderr.String()
	case err != nil:
		t.Fatalf("lit2 %q did not run: %v", args, err)
	}
	return 0, stdout.String(), stderr.String()
}

// want is run's stdout, failing t unless lit2 exits status.
func (sb sandbox) want(t *testing.T, dir string, status int, args ...string) string {
	t.Helper()
	got, stdout, stderr := sb.run(t, dir, args...)
	if got != status {
		t.Fatalf("lit2 %q exited %d, stderr %q; want %d", args, got, stderr, status)
	}
	return stdout
}

// TestSmoke runs the built lit2 as a person would, outside any checkout
// and inside two not yet minted, whose workspace drafts are not in force,
// one that does not parse and one that does.
func TestSmoke(t *testing.T) {
	sb := newSandbox(t)
	broken := checkout(t, sb, "broken", "prefix = [unclosed\n")
	parses := checkout(t, sb, "parses", oneLevel)

	var statuses []int
	for _, tc := range []struct {
		args         []string
		status       int
		stdout, errs string
		dir          string
	}{
		{[]string{"--help"}, 0, "admin sync", "", sb.dir},
		{[]string{"--help"}, 0, "admin init", "", sb.dir},
		{nil, 78, "", "error (reason=store): ", sb.dir},
		{[]string{"--help"}, 0, "admin sync", "", broken},
		{[]string{"--help"}, 0, "admin sync", "", parses},
	} {
		status, stdout, stderr := sb.run(t, tc.dir, tc.args...)
		statuses = append(statuses, status)
		for _, verb := range []string{"get", "set", "add", "rm"} {
			if tc.stdout != "" && !strings.Contains(stdout, verb) {
				t.Errorf("lit2 %q does not name %s on stdout:\n%s", tc.args, verb, stdout)
			}
		}
		if status != tc.status || !holds(stdout, tc.stdout) || !strings.HasPrefix(stderr, tc.errs) || (tc.errs == "") != (stderr == "") {
			t.Errorf("lit2 %q: status %d, stdout %q, stderr %q", tc.args, status, stdout, stderr)
		}
	}

	var recorded []int
	var configs []*configLine
	days, err := filepath.Glob(filepath.Join(sb.state(), "lit", "commands", "*.jsonl"))
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
	dir := checkout(t, sb, "work", "")
	lit := func(args ...string) string { t.Helper(); return sb.want(t, dir, 0, args...) }
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
	dir := checkout(t, sb, "work", "")
	lit := func(status int, args ...string) string { t.Helper(); return sb.want(t, dir, status, args...) }
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

// checkout makes a git checkout named name in the sandbox whose
// .lit/workspace.toml is draft, or with none when draft is empty, and is
// its directory.
func checkout(t *testing.T, sb sandbox, name, draft string) string {
	t.Helper()
	dir := filepath.Join(sb.dir, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gittest.Git(t, dir, "init", "-q")
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
