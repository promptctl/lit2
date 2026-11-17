package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSmoke builds lit2 with the documented command and runs it as a person
// would, outside any checkout and inside two whose workspace drafts are not in
// force, one that does not parse and one that does: the one end-to-end test.
func TestSmoke(t *testing.T) {
	tmp := t.TempDir()
	bin, state := filepath.Join(tmp, "lit2"), filepath.Join(tmp, "state")
	broken := checkoutWithDraft(t, filepath.Join(tmp, "broken"), "prefix = [unclosed\n")
	parses := checkoutWithDraft(t, filepath.Join(tmp, "parses"), oneLevel)
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("CGO_ENABLED=0 go build ./cmd/lit2: %v\n%s", err, out)
	}

	var statuses []int
	for _, tc := range []struct {
		args         []string
		status       int
		stdout, errs string
		brokenPipe   bool
		dir          string
	}{
		{nil, 0, "admin sync", "", false, tmp},
		{[]string{"--help"}, 0, "admin init", "", false, tmp},
		{[]string{"frobnicate"}, 64, "", "error (reason=unknown_verb): ", false, tmp},
		{nil, 0, "", "", true, tmp},
		{nil, 0, "admin sync", "", false, broken},
		{nil, 0, "admin sync", "", false, parses},
	} {
		cmd := exec.Command(bin, tc.args...)
		cmd.Dir, cmd.Env = tc.dir, append(os.Environ(), "XDG_STATE_HOME="+state)
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
		if status != tc.status || !strings.Contains(stdout.String(), tc.stdout) || !strings.HasPrefix(stderr.String(), tc.errs) || (tc.errs == "") != (stderr.Len() == 0) {
			t.Errorf("lit2 %q: status %d, stdout %q, stderr %q", tc.args, status, stdout.String(), stderr.String())
		}
	}

	var recorded []int
	var configs []configLine
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
				ExitCode int        `json:"exit_code"`
				Outcome  string     `json:"outcome"`
				Config   configLine `json:"config"`
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
	// The runs in the checkouts read the shipped default, its three levels, and
	// never the draft beside them (architecture.md §3.15): not the one that
	// parses, and not the default in place of the one that does not.
	for _, inCheckout := range configs[len(configs)-2:] {
		if inCheckout.Source != "default" || inCheckout.Counts.Levels != 3 {
			t.Errorf("a run beside a draft records config %+v; want the default's three levels", inCheckout)
		}
	}
}

// configLine is the command log's config block, as a reader of the log sees it.
type configLine struct {
	Source string `json:"source"`
	Counts struct {
		Levels int `json:"levels"`
	} `json:"counts"`
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

// checkoutWithDraft makes a git checkout at dir whose .lit/workspace.toml is
// draft, and returns dir.
func checkoutWithDraft(t *testing.T, dir, draft string) string {
	t.Helper()
	if out, err := exec.Command("git", "init", "--quiet", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
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
