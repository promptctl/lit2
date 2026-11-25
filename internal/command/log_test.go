//go:build integration

package command

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/commandname"
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
	wrote := make(chan Written, n)
	for i := range n {
		wg.Go(func() {
			e := Event{Invocation: inv, Command: summary(commandname.Help()), Place: here, Outcome: cli.Done{}}
			e.Trace = TraceID{byte(i)}
			wrote <- log.Write(e)
		})
	}
	wg.Wait()
	close(wrote)
	for w := range wrote {
		if !reflect.DeepEqual(w, Kept{}) {
			t.Fatalf("a parallel write = %#v; want it kept, nothing left", w)
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
	// notes.txt is no day file: pruning keeps it and says so
	w := NewLog(dir).Write(Event{Invocation: inv, Command: summary(commandname.Help()), Place: here, Outcome: cli.Done{}})
	if !reflect.DeepEqual(w, Kept{Left: []Unpruned{NotADayFile{Name: "notes.txt"}}}) {
		t.Fatalf("a write beside notes.txt = %#v; want it kept, saying notes.txt is no day file", w)
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
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("stat %s: %v", name, err)
		}
		if exists := err == nil; exists != kept {
			t.Errorf("%s exists: %v; want %v", name, exists, kept)
		}
	}
}

func TestALogUnderARegularFileFailsToWrite(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	later := inv
	later.Started = t0.Add(time.Hour)
	e := Event{Invocation: later, Command: summary(commandname.Help()), Place: here, Outcome: cli.Done{}}
	if w := NewLog(filepath.Join(filepath.Dir(file), "commands")).Write(e); !reflect.DeepEqual(w, Kept{}) {
		t.Fatalf("control: a log beside the regular file: %#v", w)
	}
	if lost, ok := NewLog(filepath.Join(file, "commands")).Write(e).(Lost); !ok || !errors.Is(lost.Why, syscall.ENOTDIR) {
		t.Errorf("a log directory under a regular file = %#v; want it lost for ENOTDIR, the OS refusing a directory there", lost)
	}
}

// What pruning left is each thing it left: a listing that failed is no
// failed removal, and every removal that failed is said, not the first.
func TestPruningSaysEachThingItLeft(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root lists and removes whatever a directory's mode")
	}
	e := Event{Invocation: inv, Command: summary(commandname.Help()), Place: here, Outcome: cli.Done{}}
	unlisted := t.TempDir()
	unremoved := t.TempDir()
	for _, name := range []string{"2026-09-01.jsonl", "2026-09-02.jsonl", "2026-11-11.jsonl"} {
		if err := os.WriteFile(filepath.Join(unremoved, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for dir, mode := range map[string]os.FileMode{unlisted: 0o300, unremoved: 0o500} {
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Chmod(dir, 0o700); err != nil {
				t.Error(err)
			}
		})
	}
	kept, ok := NewLog(unlisted).Write(e).(Kept)
	if !ok || len(kept.Left) != 1 || !strings.HasPrefix(kept.Left[0].String(), "listing it failed") {
		t.Fatalf("a write to a directory that does not list = %#v; want it kept, saying the listing failed", kept)
	}
	if u, isUnlisted := kept.Left[0].(Unlisted); !isUnlisted || !errors.Is(u.Err, fs.ErrPermission) {
		t.Errorf("what pruning left of a directory that does not list is %#v; want Unlisted, for its permission", kept.Left[0])
	}
	kept, ok = NewLog(unremoved).Write(e).(Kept)
	var names []string
	for _, left := range kept.Left {
		if u, isUnremoved := left.(Unremoved); isUnremoved && errors.Is(u.Err, fs.ErrPermission) {
			names = append(names, u.Name)
		}
	}
	if !ok || !reflect.DeepEqual(names, []string{"2026-09-01.jsonl", "2026-09-02.jsonl"}) || len(kept.Left) != 2 {
		t.Errorf("a write beside two old day files that will not go = %#v; want both said", kept)
	}
}

// Only the append says whether the event is in the log: an event that
// panics to encode is lost, and said with its panic.
func TestAnEventThatPanicsToEncodeIsLost(t *testing.T) {
	dir := t.TempDir()
	lost, ok := NewLog(dir).Write(Event{Invocation: inv, Command: summary(commandname.Get()), Place: here, Outcome: cli.Failed{Failure: unsayable{}}}).(Lost)
	var in Internal
	if !ok || !errors.As(lost.Why, &in) || !strings.Contains(in.Error(), "no words for it") || len(in.Stack()) == 0 {
		t.Errorf("an event that panics to encode = %#v; want it lost for its panic", lost)
	}
}
