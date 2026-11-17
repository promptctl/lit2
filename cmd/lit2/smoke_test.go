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
// would, outside any checkout and inside one whose workspace draft does not
// parse: the one end-to-end test.
func TestSmoke(t *testing.T) {
	tmp := t.TempDir()
	bin, state := filepath.Join(tmp, "lit2"), filepath.Join(tmp, "state")
	checkout := draftThatDoesNotParse(t, filepath.Join(tmp, "checkout"))
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
		{nil, 0, "admin sync", "", false, checkout},
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
	// The run in the checkout read the shipped default and never the draft
	// beside it (architecture.md §3.15).
	if inCheckout := configs[len(configs)-1]; inCheckout.Source != "default" {
		t.Errorf("the run beside a broken draft records config source %q; want default", inCheckout.Source)
	}
}

// configLine is the command log's config block, as a reader of the log sees it.
type configLine struct {
	Source string `json:"source"`
}

// draftThatDoesNotParse makes a git checkout at dir whose .lit/workspace.toml
// is not TOML, and returns dir.
func draftThatDoesNotParse(t *testing.T, dir string) string {
	t.Helper()
	if out, err := exec.Command("git", "init", "--quiet", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".lit"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".lit", "workspace.toml"), []byte("prefix = [unclosed\n"), 0o644); err != nil {
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
