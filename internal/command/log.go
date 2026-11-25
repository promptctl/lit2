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
// old day files failed: a fact of its own, not a lost record. Write builds
// it from the removal's error.
type Housekeeping struct{ err error }

func (h Housekeeping) Error() string { return h.err.Error() }

func (h Housekeeping) Unwrap() error { return h.err }

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
		return Housekeeping{err: err}
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
	Config     *configLine  `json:"config,omitempty"`
	Store      *storeLine   `json:"store,omitempty"`
	Fold       []foldLine   `json:"fold,omitempty"`
	Verbs      *verbsLine   `json:"verbs,omitempty"`
	Policy     *policyLine  `json:"policy,omitempty"`
	Git        []spawnLine  `json:"git"`
	Place      any          `json:"place"`
	Agent      string       `json:"agent,omitempty"`
}

// spawnLine is one git process the invocation ran; the block is empty,
// never absent, when none ran.
// It carries its exit status, or why it never started.
type spawnLine struct {
	Args      []string `json:"args"`
	Exit      *int     `json:"exit,omitempty"`
	Unstarted string   `json:"unstarted,omitempty"`
	Stderr    string   `json:"stderr,omitempty"`
}

func gitBlock(spawns []git.Spawn) []spawnLine {
	l := make([]spawnLine, len(spawns))
	for i, s := range spawns {
		l[i] = spawnLine{Args: s.Args, Stderr: s.Stderr}
		switch e := s.Ended.(type) {
		case git.Exited:
			l[i].Exit = &e.Code
		case git.Unstarted:
			l[i].Unstarted = e.Why.Error()
		default:
			panic(fmt.Sprintf("command: %T is not how a spawn ended", e))
		}
	}
	return l
}

type commandFacts struct {
	Paths []cli.PathText `json:"paths"`
}

type counts struct {
	Paths  int `json:"paths"`
	Values int `json:"values"`
}

// configLine is the config block: the version in force a run read and what
// it held. Every count is written, zeros included, so an absent block and an
// empty version are different facts.
type configLine struct {
	Ref    string       `json:"ref"`
	Gate   string       `json:"gate"`
	Counts configCounts `json:"counts"`
}

type configCounts struct {
	Levels       int `json:"levels"`
	Columns      int `json:"columns"`
	Labels       int `json:"labels"`
	Requirements int `json:"requirements"`
	Checks       int `json:"checks"`
	Links        int `json:"links"`
	Sections     int `json:"sections"`
}

func configBlock(c *ConfigFacts) *configLine {
	if c == nil {
		return nil
	}
	n := c.Facts.Counts
	return &configLine{
		Ref:  c.Ref,
		Gate: c.Facts.Gate,
		Counts: configCounts{
			Levels: n.Levels, Columns: n.Columns, Labels: n.Labels, Requirements: n.Requirements,
			Checks: n.Checks, Links: n.Links, Sections: n.Sections,
		},
	}
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
		Config:     configBlock(e.Config),
		Store:      storeBlock(e.Store),
		Fold:       foldBlock(e.Store.Folds),
		Verbs:      verbsBlock(e.Verbs, e.Store.Landed()),
		Policy:     policyBlock(e.Policy),
		Git:        gitBlock(e.Git),
		Place:      placeLine(e.Place),
	}
	switch o := e.Outcome.(type) {
	case cli.Done:
		l.Outcome = "ok"
	case cli.Failed:
		l.Outcome, l.Reason, l.Error = o.Failure.Class().String(), o.Failure.Reason(), o.Failure.Error()
		if in, ok := o.Failure.(Internal); ok {
			l.Stack = string(in.Stack())
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
		return inCheckoutLine{checkoutLine{WorkTree: c.WorkTree, GitDir: c.GitDir, CommonDir: c.CommonDir, ObjectFormat: c.Format.String(), Identity: identityLine(p.Identity, c), Temps: tempsLine(p.Temps)}}
	case NoCheckout:
		return noCheckoutLine{dirLine(p)}
	case Unlocatable:
		return unlocatableLine{dirErrorLine{p.Dir, p.Err.Error()}}
	}
	panic(fmt.Sprintf("command: %T is not a Place", p))
}

// tempsLine is each file's leftover temps, or why they did not read.
func tempsLine(ls []Leftovers) []any {
	l := []any{}
	for _, left := range ls {
		switch left := left.(type) {
		case TempsBeside:
			l = append(l, tempsBesideLine{Path: left.Path, Temps: append([]string{}, left.Temps...)})
		case TempsUnread:
			l = append(l, pathErrorLine{Path: left.Path, Error: left.Why.Error()})
		}
	}
	return l
}

// identityLine is the writer and its principal as did:key, or the paths
// missing, or the path that does not read and why; never the key's seed,
// which no Key prints.
func identityLine(id Identity, c git.Checkout) any {
	switch id := id.(type) {
	case Minted:
		return mintedLine{Checkout: id.Checkout.String(), Principal: id.Key.Principal().String()}
	case Unminted:
		return unmintedLine{id.Paths(c)}
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
		WorkTree     string `json:"work_tree"`
		GitDir       string `json:"git_dir"`
		CommonDir    string `json:"common_dir"`
		ObjectFormat string `json:"object_format"`
		Identity     any    `json:"identity"`
		Temps        []any  `json:"temps"`
	}
	tempsBesideLine struct {
		Path  string   `json:"path"`
		Temps []string `json:"temps"`
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
