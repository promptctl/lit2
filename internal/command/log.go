package command

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/events"
)

// Sink is where command events go: the one export edge (D13).
type Sink interface{ Write(Event) error }

// retention is how long a day file of the command log is kept (D12).
const retention = 30 * 24 * time.Hour

// NewLog is the command log in dir: one file per UTC day, <date>.jsonl,
// one line per event (D12).
func NewLog(dir string) Sink { return commandLog{dir: dir} }

type commandLog struct{ dir string }

// Write removes the day files more than 30 days older than the event's day,
// then appends the event's line in one O_APPEND write, so concurrent
// invocations never interleave. Any failure is returned and the event is
// then not in the log, which is what the caller reports.
func (l commandLog) Write(e Event) error {
	line, err := encode(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return err
	}
	day := e.Started.UTC().Truncate(24 * time.Hour)
	if err := l.prune(day.Add(-retention)); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(l.dir, day.Format(time.DateOnly)+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(line, '\n'))
	return errors.Join(werr, f.Close())
}

// prune removes every day file dated before cutoff. A file another
// invocation removed first is gone, which is what pruning asks for.
func (l commandLog) prune(cutoff time.Time) error {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		day, err := time.Parse(time.DateOnly+".jsonl", entry.Name())
		if err != nil || !day.Before(cutoff) {
			continue // not a day file, or not yet 30 days old
		}
		if err := os.Remove(filepath.Join(l.dir, entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// Memory is a sink that keeps every event in order, for tests.
type Memory struct{ Events []Event }

// Write appends e.
func (m *Memory) Write(e Event) error {
	m.Events = append(m.Events, e)
	return nil
}

// Unwritable is the sink of a command log whose directory cannot be
// resolved: every write fails with Err, so the loss is said on every
// invocation rather than once.
type Unwritable struct{ Err error }

// Write fails with Err.
func (u Unwritable) Write(Event) error { return u.Err }

// line is the command event as one JSON object: the default field set,
// then lit2's own fields. Counts are written when zero, so a run that did
// nothing is a line with zeros and a run that never happened is no line
// `[LAW:nothing-unseen]`.
type line struct {
	Event      string         `json:"event"`
	TraceID    string         `json:"trace_id"`
	Service    string         `json:"service"`
	StartedAt  string         `json:"started_at"`
	DurationMS float64        `json:"duration_ms"`
	Outcome    string         `json:"outcome"`
	Error      string         `json:"error,omitempty"`
	Reason     string         `json:"reason,omitempty"`
	Stack      string         `json:"stack,omitempty"`
	ExitCode   int            `json:"exit_code"`
	Version    string         `json:"version"`
	Command    commandFacts   `json:"command"`
	Counts     counts         `json:"counts"`
	Place      map[string]any `json:"place"`
	Agent      string         `json:"agent,omitempty"`
}

type commandFacts struct {
	Paths []cli.PathText `json:"paths"`
}

type counts struct {
	Paths  int `json:"paths"`
	Values int `json:"values"`
}

func encode(e Event) ([]byte, error) {
	l := line{
		Event:      "lit2 " + string(e.Command.Name),
		TraceID:    e.Trace.String(),
		Service:    "lit2",
		StartedAt:  e.Started.UTC().Format(time.RFC3339Nano),
		DurationMS: float64(e.Duration) / float64(time.Millisecond),
		ExitCode:   cli.ExitCode(e.Outcome),
		Version:    e.Version,
		Command:    commandFacts{Paths: append([]cli.PathText{}, e.Command.Paths...)},
		Counts:     counts{Paths: len(e.Command.Paths), Values: e.Command.Values},
		Place:      placeFacts(e.Place),
	}
	switch o := e.Outcome.(type) {
	case cli.Done:
		l.Outcome = "ok"
	case cli.Failed:
		l.Outcome, l.Reason, l.Error = o.Failure.Class().String(), o.Failure.Reason(), o.Failure.Error()
		if in, ok := o.Failure.(Internal); ok {
			l.Stack = string(in.Stack)
		}
	}
	switch a := e.Agent.(type) {
	case events.Labeled:
		l.Agent = a.String()
	case events.Unlabeled:
	}
	b, err := json.Marshal(l)
	if err != nil {
		return nil, fmt.Errorf("encoding the command event: %w", err)
	}
	return b, nil
}

func placeFacts(p Place) map[string]any {
	switch p := p.(type) {
	case InCheckout:
		return map[string]any{"checkout": map[string]any{
			"work_tree":  p.Checkout.WorkTree,
			"git_dir":    p.Checkout.GitDir,
			"common_dir": p.Checkout.CommonDir,
			"writer":     writerFacts(p.Writer),
		}}
	case NoCheckout:
		return map[string]any{"outside": map[string]any{"dir": p.Dir}}
	case Unlocatable:
		return map[string]any{"unlocatable": map[string]any{"dir": p.Dir, "error": p.Err.Error()}}
	}
	panic(fmt.Sprintf("command: %T is not a Place", p))
}

func writerFacts(w Writer) map[string]any {
	switch w := w.(type) {
	case Minted:
		return map[string]any{"id": w.ID.String()}
	case Unminted:
		return map[string]any{"unminted": map[string]any{"path": w.Path}}
	case Unreadable:
		return map[string]any{"unreadable": map[string]any{"path": w.Path, "error": w.Err.Error()}}
	}
	panic(fmt.Sprintf("command: %T is not a Writer", w))
}
