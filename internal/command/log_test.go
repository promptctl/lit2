package command

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/cli"
)

func lines(t *testing.T, path string) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for l := range bytes.Lines(b) {
		var m map[string]any
		if err := json.Unmarshal(l, &m); err != nil {
			t.Fatalf("line %q does not decode: %v", l, err)
		}
		out = append(out, m)
	}
	return out
}

func TestParallelWritersLeaveEveryLineWhole(t *testing.T) {
	dir := t.TempDir()
	log := NewLog(dir)
	const n = 64
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Go(func() {
			e := Event{Invocation: inv, Command: cli.Summary{Name: "help"}, Started: t0, Outcome: cli.Done{}}
			e.Trace = TraceID{byte(i)}
			errs <- log.Write(e)
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	traces := map[any]bool{}
	for _, m := range lines(t, filepath.Join(dir, "2026-11-11.jsonl")) {
		traces[m["trace_id"]] = true
	}
	if len(traces) != n {
		t.Errorf("%d distinct events in the log; want %d", len(traces), n)
	}
}

func TestAWriteRemovesDayFilesOlderThan30Days(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"2026-10-01.jsonl", "2026-10-11.jsonl", "2026-10-12.jsonl", "2026-10-13.jsonl", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := NewLog(dir).Write(Event{Invocation: inv, Command: cli.Summary{Name: "help"}, Started: t0, Outcome: cli.Done{}}); err != nil {
		t.Fatal(err)
	}
	for name, kept := range map[string]bool{
		"2026-10-01.jsonl": false, // 41 days old
		"2026-10-11.jsonl": false, // 31 days old
		"2026-10-12.jsonl": true,  // 30 days old
		"2026-10-13.jsonl": true,
		"notes.txt":        true, // not a day file
		"2026-11-11.jsonl": true, // the day the event was written
	} {
		_, err := os.Stat(filepath.Join(dir, name))
		if exists := err == nil; exists != kept {
			t.Errorf("%s exists: %v; want %v (%v)", name, exists, kept, err)
		}
	}
}

func TestALogUnderARegularFileFailsToWrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	e := Event{Invocation: inv, Command: cli.Summary{Name: "help"}, Started: t0.Add(time.Hour), Outcome: cli.Done{}}
	if err := NewLog(filepath.Join(file, "commands")).Write(e); err == nil {
		t.Error("a log directory under a regular file was written")
	}
}
