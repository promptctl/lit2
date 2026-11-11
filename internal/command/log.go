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
	"github.com/promptctl/lit2/internal/git"
)

// Sink is where command events go: the one export edge (D13).
type Sink interface{ Write(Event) error }

// retention is how long a day file of the command log is kept (D12).
const retention = 30 * 24 * time.Hour

// NewLog is the command log in dir: one file per UTC day, <date>.jsonl,
// one line per event (D12).
func NewLog(dir string) Sink { return commandLog{dir: dir} }

type commandLog struct{ dir string }

// Housekeeping is a write whose event is in the log and whose removal of
// old day files failed: a fact of its own, not a lost record.
type Housekeeping struct{ Err error }

func (h Housekeeping) Error() string { return h.Err.Error() }

// Write appends the event's line in one O_APPEND write, so concurrent
// invocations never interleave, then removes the day files more than 30
// days older than the event's day. The record is the job, so it comes
// first: a failed removal is a Housekeeping, and any other failure means
// the event is not in the log.
func (l commandLog) Write(e Event) error {
	line, err := encode(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return err
	}
	day := e.Started.UTC().Truncate(24 * time.Hour)
	f, err := os.OpenFile(filepath.Join(l.dir, day.Format(time.DateOnly)+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(line, '\n'))
	if err := errors.Join(werr, f.Close()); err != nil {
		return err
	}
	if err := l.prune(day.Add(-retention)); err != nil {
		return Housekeeping{Err: err}
	}
	return nil
}

// prune removes every day file dated before cutoff. A day file is a
// regular file so named. A file another invocation removed first is gone,
// which is what pruning asks for.
func (l commandLog) prune(cutoff time.Time) error {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		day, err := time.Parse(time.DateOnly+".jsonl", entry.Name())
		if err != nil || !entry.Type().IsRegular() || !day.Before(cutoff) {
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
	Event      string       `json:"event"`
	TraceID    string       `json:"trace_id"`
	Service    string       `json:"service"`
	StartedAt  string       `json:"started_at"`
	DurationMS float64      `json:"duration_ms"`
	Outcome    string       `json:"outcome"`
	Error      string       `json:"error,omitempty"`
	Reason     string       `json:"reason,omitempty"`
	Stack      string       `json:"stack,omitempty"`
	ExitCode   int          `json:"exit_code"`
	Version    string       `json:"version"`
	Command    commandFacts `json:"command"`
	Counts     counts       `json:"counts"`
	Store      *storeLine   `json:"store,omitempty"`
	Place      any          `json:"place"`
	Agent      string       `json:"agent,omitempty"`
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
		Store:      storeBlock(e.Store),
		Place:      placeLine(e.Place),
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

func placeLine(p Place) any {
	switch p := p.(type) {
	case InCheckout:
		c := p.Checkout
		return inCheckoutLine{checkoutLine{WorkTree: c.WorkTree, GitDir: c.GitDir, CommonDir: c.CommonDir, Identity: identityLine(p.Identity, c)}}
	case NoCheckout:
		return noCheckoutLine{dirLine(p)}
	case Unlocatable:
		return unlocatableLine{dirErrorLine{p.Dir, p.Err.Error()}}
	}
	panic(fmt.Sprintf("command: %T is not a Place", p))
}

// identityLine is the writer and its principal as did:key, or the paths
// missing, or the path that does not read and why; never the key's seed,
// which no Key prints.
func identityLine(id Identity, c git.Checkout) any {
	switch id := id.(type) {
	case Minted:
		return mintedLine{Checkout: id.Checkout.String(), Principal: id.Key.Principal().String()}
	case Unminted:
		paths := []string{}
		for _, f := range id.Files() {
			paths = append(paths, PathOf(f, c))
		}
		return unmintedLine{paths}
	case Unreadable:
		return unreadableLine{pathErrorLine{PathOf(id.File, c), id.Err.Error()}}
	}
	panic(fmt.Sprintf("command: %T is not an Identity", id))
}

// The place and the identity as the line spells them: one struct per
// variant, keyed by the variant's wire name, so the shape has one typed
// source.
type (
	inCheckoutLine struct {
		Checkout checkoutLine `json:"checkout"`
	}
	noCheckoutLine struct {
		Outside dirLine `json:"outside"`
	}
	unlocatableLine struct {
		Unlocatable dirErrorLine `json:"unlocatable"`
	}
	mintedLine struct {
		Checkout  string `json:"checkout"`
		Principal string `json:"principal"`
	}
	unmintedLine struct {
		Unminted []string `json:"unminted"`
	}
	unreadableLine struct {
		Unreadable pathErrorLine `json:"unreadable"`
	}

	checkoutLine struct {
		WorkTree  string `json:"work_tree"`
		GitDir    string `json:"git_dir"`
		CommonDir string `json:"common_dir"`
		Identity  any    `json:"identity"`
	}
	dirLine struct {
		Dir string `json:"dir"`
	}
	dirErrorLine struct {
		Dir   string `json:"dir"`
		Error string `json:"error"`
	}
	pathErrorLine struct {
		Path  string `json:"path"`
		Error string `json:"error"`
	}
)
