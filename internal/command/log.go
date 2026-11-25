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
type Sink interface{ Write(Event) Written }

// Written is what writing an event did: Kept, the event in the log, with
// what pruning left; or Lost, the event not in the log, and why.
//
//sumtype:decl
type Written interface{ written() }

// Kept is an event in the log, and Left, what pruning left beside it,
// none or some, each said on its own `[LAW:nothing-unseen]`.
type Kept struct{ Left []Unpruned }

// Lost is an event that is not in the log, and why.
type Lost struct{ Why error }

func (Kept) written() {}
func (Lost) written() {}

// Unpruned is something pruning left: a listing of the log's directory
// that failed, so nothing old was found; an old day file whose removal
// failed, so the log keeps more than its retention; an entry that is not
// a day file, which pruning never removes. String says it.
//
//sumtype:decl
type Unpruned interface {
	unpruned()
	String() string
}

// Unlisted is a log directory whose listing failed.
type Unlisted struct{ Err error }

// Unremoved is an old day file, Name, whose removal failed.
type Unremoved struct {
	Name string
	Err  error
}

// NotADayFile is an entry, Name, that is not a day file.
type NotADayFile struct{ Name string }

func (Unlisted) unpruned()    {}
func (Unremoved) unpruned()   {}
func (NotADayFile) unpruned() {}

func (u Unlisted) String() string {
	return "listing it failed, so no old day file was pruned: " + u.Err.Error()
}

func (u Unremoved) String() string {
	return fmt.Sprintf("removing %s, older than its retention, failed: %v", u.Name, u.Err)
}

func (n NotADayFile) String() string {
	return fmt.Sprintf("kept %s: not a day file, so never pruned", n.Name)
}

// retention is how long a day file of the command log is kept (D12).
const retention = 30 * 24 * time.Hour

// NewLog is the command log in lit/commands under the state directory
// XDG_STATE_HOME, stateHome, names, or home's .local/state when it names
// none: one file per UTC day, <date>.jsonl, one line per event (D12). The
// XDG spec makes a relative value invalid, to be ignored, so an empty or a
// relative value is ignored as an unset one is, and each line the log
// writes says which it was. A home that does not read is a log every write
// loses, saying why.
func NewLog(stateHome Variable, home func() (string, error)) Sink {
	state, chose := "", stateHomeLine{XDGStateHome: "used"}
	switch v := stateHome.(type) {
	case Unset:
		chose.XDGStateHome = "unset"
	case Set:
		switch {
		case v.Value == "":
			chose.XDGStateHome = "empty"
		case !filepath.IsAbs(v.Value):
			chose = stateHomeLine{XDGStateHome: "relative", Ignored: v.Value}
		default:
			state = v.Value
		}
	}
	if chose.XDGStateHome != "used" {
		h, err := home()
		if err != nil {
			return Unwritable{Err: fmt.Errorf("no directory for the command log: XDG_STATE_HOME is %s, and %w", chose, err)}
		}
		state = filepath.Join(h, ".local", "state")
	}
	return commandLog{dir: filepath.Join(state, "lit", "commands"), chose: chose}
}

// stateHomeLine is how the log's directory was chosen, as each line it
// writes says it: XDG_STATE_HOME used, or unset, empty or relative, and so
// ignored, a relative value said with it.
type stateHomeLine struct {
	XDGStateHome string `json:"xdg_state_home"`
	Ignored      string `json:"ignored,omitempty"`
}

func (s stateHomeLine) String() string {
	if s.Ignored != "" {
		return fmt.Sprintf("%s (%q)", s.XDGStateHome, s.Ignored)
	}
	return s.XDGStateHome
}

type commandLog struct {
	dir   string
	chose stateHomeLine
}

// Write appends the event's line in one O_APPEND write, so concurrent
// invocations never interleave, then prunes the day files more than 30
// days older than the event's day. The record is the job, so it comes
// first: once it is in, the write is Kept, with what pruning left.
func (l commandLog) Write(e Event) Written {
	day := e.Started.UTC().Truncate(24 * time.Hour)
	if err := l.append(e, day); err != nil {
		return Lost{Why: err}
	}
	return Kept{Left: l.prune(day.Add(-retention))}
}

// append is e's line, which says how the log was chosen, appended to the
// file of its day.
func (l commandLog) append(e Event, day time.Time) error {
	line, err := encodeIn(e, &l.chose)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(l.dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(l.dir, day.Format(time.DateOnly)+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(line, '\n'))
	return errors.Join(werr, f.Close())
}

// prune removes every day file dated before cutoff, a day file being a
// regular file so named, and is what it left: a listing that failed, or
// every removal that failed and every entry that is not a day file. A
// file another invocation removed first is gone, which is what pruning
// asks for.
func (l commandLog) prune(cutoff time.Time) []Unpruned {
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return []Unpruned{Unlisted{Err: err}}
	}
	var left []Unpruned
	for _, entry := range entries {
		day, err := time.Parse(time.DateOnly+".jsonl", entry.Name())
		if err != nil || !entry.Type().IsRegular() {
			left = append(left, NotADayFile{Name: entry.Name()})
			continue
		}
		if !day.Before(cutoff) {
			continue // not yet 30 days old
		}
		if err := os.Remove(filepath.Join(l.dir, entry.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			left = append(left, Unremoved{Name: entry.Name(), Err: err})
		}
	}
	return left
}

// Memory is a sink that keeps every event in order, for tests.
type Memory struct{ Events []Event }

// Write appends e.
func (m *Memory) Write(e Event) Written {
	m.Events = append(m.Events, e)
	return Kept{}
}

// Unwritable is the sink of a command log whose directory cannot be
// resolved: every write is Lost for Err, so the loss is said on every
// invocation rather than once.
type Unwritable struct{ Err error }

// Write is Lost for Err.
func (u Unwritable) Write(Event) Written { return Lost{Why: u.Err} }

// line is the command event as one JSON object: the default field set,
// then lit2's own fields; the error line's delivery, with why when it was
// not delivered; the agent, or why there is none, LIT_AGENT unset or set
// empty; and how the log that wrote it was chosen, which a sink that
// writes no line has none of. Counts are written when zero, so a run that did
// nothing is a line with zeros and a run that never happened is no line
// `[LAW:nothing-unseen]`.
type line struct {
	Event                string         `json:"event"`
	TraceID              string         `json:"trace_id"`
	Service              string         `json:"service"`
	StartedAt            string         `json:"started_at"`
	DurationMS           float64        `json:"duration_ms"`
	Outcome              string         `json:"outcome"`
	Error                string         `json:"error,omitempty"`
	Reason               string         `json:"reason,omitempty"`
	Stack                string         `json:"stack,omitempty"`
	ExitCode             int            `json:"exit_code"`
	ErrorLine            string         `json:"error_line"`
	ErrorLineUndelivered string         `json:"error_line_undelivered,omitempty"`
	Version              string         `json:"version"`
	Command              commandFacts   `json:"command"`
	Counts               counts         `json:"counts"`
	Config               *configLine    `json:"config,omitempty"`
	Store                storeLine      `json:"store"`
	Fold                 []foldLine     `json:"fold,omitempty"`
	Verbs                *verbsLine     `json:"verbs,omitempty"`
	Policy               *policyLine    `json:"policy,omitempty"`
	Git                  []spawnLine    `json:"git"`
	Place                any            `json:"place"`
	Agent                string         `json:"agent,omitempty"`
	AgentUnlabeled       string         `json:"agent_unlabeled,omitempty"`
	CommandLog           *stateHomeLine `json:"command_log,omitempty"`
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

func encode(e Event) ([]byte, error) { return encodeIn(e, nil) }

// encodeIn is e's line as the log chosen as chose writes it.
func encodeIn(e Event, chose *stateHomeLine) ([]byte, error) {
	l := line{
		CommandLog: chose,
		Event:      "lit2 " + e.Command.Name.String(),
		TraceID:    e.Trace.String(),
		Service:    "lit2",
		StartedAt:  e.Started.UTC().Format(time.RFC3339Nano),
		DurationMS: float64(e.Duration) / float64(time.Millisecond),
		ExitCode:   cli.ExitCode(e.Outcome),
		Version:    e.Version,
		Command:    commandFacts{Paths: append([]cli.PathText{}, e.Command.Paths...)},
		Counts:     counts{Paths: len(e.Command.Paths), Values: e.Command.Values},
		Config:     configBlock(e.Config),
		Store:      storeBlock(openingOf(e.Place), e.Store),
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
		l.Stack = string(stackOf(o))
	}
	switch d := e.ErrorLine.(type) {
	case NoErrorLine:
		l.ErrorLine = "none"
	case LineDelivered:
		l.ErrorLine = "delivered"
	case LineUndelivered:
		l.ErrorLine, l.ErrorLineUndelivered = "undelivered", d.Why.Error()
	}
	switch v := e.LitAgent.(type) {
	case Unset:
		l.AgentUnlabeled = "unset"
	case Set:
		switch a := events.AgentFrom(v.Value).(type) {
		case events.Labeled:
			l.Agent = a.String()
		case events.Unlabeled:
			l.AgentUnlabeled = "empty"
		}
	}
	b, err := json.Marshal(l)
	if err != nil {
		return nil, fmt.Errorf("encoding the command event: %w", err)
	}
	return b, nil
}

// placeLine is how far a run got in reading its place, as the line spells
// it: unread; a checkout found, its identity unread; or the place it read.
func placeLine(w Where) any {
	switch w := w.(type) {
	case PlaceUnread:
		return unreadLine{}
	case AtCheckout:
		return inCheckoutLine{checkoutLineOf(w.Checkout, unreadLine{})}
	case InCheckout:
		return inCheckoutLine{checkoutLineOf(w.Checkout, identityLine(w.Identity, w.Checkout))}
	case NoCheckout:
		return noCheckoutLine{dirLine(w)}
	case Unlocatable:
		return unlocatableLine{dirErrorLine{w.Dir, w.Err.Error()}}
	}
	panic(fmt.Sprintf("command: %T is not a Where", w))
}

func checkoutLineOf(c git.Checkout, identity any) checkoutLine {
	return checkoutLine{WorkTree: c.WorkTree, GitDir: c.GitDir, CommonDir: c.CommonDir, ObjectFormat: c.Format.String(), Identity: identity}
}

// openingOf is what a run that got as far as w opened: what a place it
// read opens, and no store where it ended before reading one.
func openingOf(w Where) Opening {
	switch w := w.(type) {
	case PlaceUnread:
		return NoStore{Why: errors.New("the run ended before git said where it is")}
	case AtCheckout:
		return NoStore{Why: errors.New("the run ended before its checkout's identity was read")}
	case InCheckout:
		return OpeningAt(w)
	case NoCheckout:
		return OpeningAt(w)
	case Unlocatable:
		return OpeningAt(w)
	}
	panic(fmt.Sprintf("command: %T is not a Where", w))
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
	unreadLine struct {
		Unread struct{} `json:"unread"`
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
