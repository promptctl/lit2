// Package command is the command event, the one record each invocation
// leaves in this machine's command log.
//
// It sits beside the stack (architecture.md §6) and is §5 step 8, Record:
// one wide event per invocation, local, never synced `[LAW:nothing-unseen]`.
// Record runs every invocation's stages, so each ends Recorded, its event
// made with every fact its stages gave before it ended, or Unrecorded, when
// a read the event is made of broke and there is no event to make.
package command

import (
	"encoding/hex"
	"errors"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/reason"
)

// Clock is the one source of time; the root reads it, never the core
// `[LAW:effects-at-boundaries]`.
type Clock interface{ Now() time.Time }

// FixedClock is a clock that reads At until a test moves it.
type FixedClock struct{ At time.Time }

// Now is At.
func (c *FixedClock) Now() time.Time { return c.At }

// TraceID correlates everything one invocation did; it is minted from
// entropy, one per invocation.
type TraceID [16]byte

func (t TraceID) String() string { return hex.EncodeToString(t[:]) }

// Invocation is what is known of an invocation before anything else is
// read: its trace, its release, LIT_AGENT as it read it, and Started, the
// clock's first reading, so the duration covers every stage of it.
type Invocation struct {
	Trace    TraceID
	Version  string
	LitAgent Variable
	Started  time.Time
}

// Variable is an environment variable as the process read it: Unset, or
// Set to its value, an empty one included, which is a different fact.
//
//sumtype:decl
type Variable interface{ variable() }

// Unset is a variable the environment does not hold.
type Unset struct{}

// Set is a variable the environment holds, and its value.
type Set struct{ Value string }

func (Unset) variable() {}
func (Set) variable()   {}

// AgentOf is the agent LIT_AGENT, v, labels a run with: none when it is
// unset, and what D7 reads from its value when it is set.
func AgentOf(v Variable) events.Agent {
	switch v := v.(type) {
	case Unset:
		return events.Unlabeled{}
	case Set:
		return events.AgentFrom(v.Value)
	}
	panic(fmt.Sprintf("command: %T is not a Variable", v))
}

// Ended is how an invocation ended: Recorded, its event made, or
// Unrecorded, with no event to make.
//
//sumtype:decl
type Ended interface{ ended() }

// Recorded is a run whose event was made: the event, and the exit status
// of its outcome, said under the recover that made it.
type Recorded struct {
	Event  Event
	Status int
}

// Unrecorded is a run that made no event: a read the event is made of, at
// its start or its end, broke, Broke. Said is what the run says of the
// outcome it had, which no event keeps: its error line, when Say did not
// deliver it, and the stack of a panic that ended it; nothing when it
// broke before it had an outcome.
type Unrecorded struct {
	Said  string
	Broke Internal
}

func (Recorded) ended()   {}
func (Unrecorded) ended() {}

// Ending is how a run ended, as its event says it: Succeeded, or Failed,
// with its failure and what became of the error line that said it, which
// only a failure has.
//
//sumtype:decl
type Ending interface{ ending() }

// Succeeded is a run that did what was asked.
type Succeeded struct{}

// Failed is a run that ended in Failure, and what Say did with its line.
type Failed struct {
	Failure reason.Failure
	Line    Delivery
}

func (Succeeded) ending() {}
func (Failed) ending()    {}

// Delivery is what Say did with a failure's error line: took it; refused
// it, and why; or panicked, with the panic and its stack.
//
//sumtype:decl
type Delivery interface{ delivery() }

// Delivered is an error line Say took.
type Delivered struct{}

// Refused is an error line Say refused, and why.
type Refused struct{ Why error }

// SayPanicked is an error line Say panicked on.
type SayPanicked struct{ Broke Internal }

func (Delivered) delivery()   {}
func (Refused) delivery()     {}
func (SayPanicked) delivery() {}

// ConfigFacts is the version in force a run read from its store's fold:
// its config-ref and what it held. How many versions the log holds, and
// how many contend, are the fold's counts.
type ConfigFacts struct {
	Facts config.Facts
	// Ref is the config-ref in hex.
	Ref string
}

// Event is the command event: the invocation, the command it asked, how
// far it got in reading its place and, once read, the place it ended in,
// which says the store it opened, how long it took, what its store steps
// did, the version it ran under, every git process it ran, and how it
// ended. Config is nil when the run read no version, which the event
// records as an absent config block; Git is empty when no git ran.
type Event struct {
	Invocation
	Command  cli.Summary
	Place    Where
	Duration time.Duration
	Store    StoreFacts
	Config   *ConfigFacts
	Verbs    *VerbsFacts
	Policy   PolicyFacts
	Git      []git.Spawn
	Ending   Ending
}

// Outcome is the outcome the run's ending is.
func (e Event) Outcome() cli.Outcome {
	switch end := e.Ending.(type) {
	case Succeeded:
		return cli.Done{}
	case Failed:
		return cli.Failed{Failure: end.Failure}
	}
	panic(fmt.Sprintf("command: %T is not an Ending", e.Ending))
}

// Stages is one invocation as Record runs it, in this order, each stage
// handed what the one before it gave `[LAW:no-ambient-temporal-coupling]`:
// Begin, the reads an event is made of; Ask, what the command line asks
// and its answer, A; Locate, the checkout git finds; Identify, the place
// that checkout is, its identity read; Answer, the answer run at that
// place; End, the steps every run ends with, over the answer's outcome;
// and Say, the outcome's error line put where the person who ran it reads
// it, before the event is written, so a slow or hung log cannot hold the
// line back.
type Stages[A any] struct {
	Begin    func() Invocation
	Ask      func() (cli.Summary, A)
	Locate   func() Located
	Identify func(Located) Place
	Answer   func(A, Run) cli.Outcome
	End      func(Run, cli.Outcome) cli.Outcome
	Say      func(line string) error
}

// Run is an invocation at its place, as Record hands it to Answer and End:
// when it started, where it is, and where it says what its steps did, each
// fact as soon as it is known, so a panic keeps the facts given before it.
// Only Record makes a Run that holds a run; its zero value holds none.
type Run struct{ r *run }

// run is what Record holds of an invocation as its stages go: what was
// asked, how far it got in reading its place, and the facts its steps
// gave.
type run struct {
	started time.Time
	asked   cli.Summary
	where   func() Where
	at      Place
	stored  StoreFacts
	policy  PolicyFacts
	config  *ConfigFacts
	did     *VerbsFacts
}

// Started is the clock's first reading, the run's now.
func (r Run) Started() time.Time { return r.r.started }

// Place is where the run is now.
func (r Run) Place() Place { return r.r.at }

// Move is the run moved to p: admin init's mint moves it, so the event
// records the place the run ended in.
func (r Run) Move(p Place) { r.r.at = p }

// Stored is the run's store steps, which each step fills in place.
func (r Run) Stored() *StoreFacts { return &r.r.stored }

// Policy is the run's policy facts, which each step fills in place.
func (r Run) Policy() *PolicyFacts { return &r.r.policy }

// Configured is the version in force, once the fold has read it.
func (r Run) Configured(c ConfigFacts) { r.r.config = &c }

// Told is what the verb did; the last told is kept.
func (r Run) Told(v VerbsFacts) { r.r.did = &v }

// Where is how far a run got in reading its place, as its event says it:
// PlaceUnread; a Located, the checkout found but its identity unread; or
// the Place it read. A run's stages after Identify see only a Place, and
// only an event holds a Where `[LAW:types-are-the-program]`.
//
//sumtype:decl
type Where interface{ where() }

// PlaceUnread is a run that ended, by a panic, before git said where it
// is.
type PlaceUnread struct{}

// Place is where an invocation ran.
//
//sumtype:decl
type Place interface {
	Where
	place()
}

// InCheckout is an invocation inside a git checkout, with its identity.
type InCheckout struct {
	Checkout git.Checkout
	Identity Identity
}

// NoCheckout is an invocation whose walk reached / with no .git.
type NoCheckout struct{ Dir string }

// Unlocatable is an invocation whose walk itself failed, which is not the
// same fact as finding no checkout.
type Unlocatable struct {
	Dir string
	Err error
}

func (InCheckout) place()  {}
func (NoCheckout) place()  {}
func (Unlocatable) place() {}

// Located is what git found where an invocation runs, before its identity
// is read: a checkout, none, or a search that failed.
//
//sumtype:decl
type Located interface {
	Where
	located()
}

// AtCheckout is a checkout git found.
type AtCheckout struct{ Checkout git.Checkout }

func (AtCheckout) located()  {}
func (NoCheckout) located()  {}
func (Unlocatable) located() {}

func (PlaceUnread) where() {}
func (InCheckout) where()  {}
func (AtCheckout) where()  {}
func (NoCheckout) where()  {}
func (Unlocatable) where() {}

// Internal is lit2 failing: an error that answers no class, a panic, a row
// whose body is not built. Outside this package it is built from its
// cause by Broke.
type Internal struct {
	err   error
	stack []byte
}

// Broke is lit2 failing with err.
func Broke(err error) Internal { return Internal{err: err} }

// Panicked is lit2 failing with a panic's value, recovered where stack was
// taken. A value that is an error stays in the chain, so errors.As finds
// it.
func Panicked(value any, stack []byte) Internal {
	if err, isErr := value.(error); isErr {
		return Internal{err: fmt.Errorf("panic: %w", err), stack: stack}
	}
	return Internal{err: fmt.Errorf("panic: %v", value), stack: stack}
}

func (i Internal) Error() string { return i.err.Error() }

func (i Internal) Unwrap() error { return i.err }

// Stack is the panic's stack, empty otherwise.
func (i Internal) Stack() []byte { return i.stack }

// Class is internal.
func (Internal) Class() reason.Class { return reason.ClassInternal }

// Reason is internal.
func (Internal) Reason() string { return "internal" }

// Record runs one invocation's stages, in order, and returns how it ended,
// on every path it takes. Each stage is caught, by Caught, and its panic
// means what its place in the run makes it `[LAW:nothing-unseen]`:
//   - a panic in Answer is its outcome, an Internal, which End runs over
//     as over any other, so a run that crashed still says the durability of
//     what it wrote and the temps it left;
//   - a panic from Ask through Identify, or in End, or in saying the
//     outcome's error line and status, is the run's outcome, an Internal,
//     recorded with the facts given before it;
//   - a panic in Say is a line it did not deliver, which the event says;
//   - a panic in Begin, or in the end's reads, the clock again for the
//     duration and the git processes the run spawned, leaves no event to
//     make: the run is Unrecorded, saying what Say did not, the line it
//     did not deliver and the stack of a panic that ended the outcome.
func Record[A any](c Clock, spawns func() []git.Spawn, s Stages[A]) Ended {
	return Caught(func() Ended {
		inv := s.Begin()
		r := &run{started: inv.Started, asked: cli.Unparsed(), where: func() Where { return PlaceUnread{} }}
		end := Caught(func() said { return saying(through(r, s)) }, func(broke Internal) said { return saying(failedFor(broke)) })
		// said before the end's reads, so the duration covers it
		ended := ending(end, s.Say)
		return Caught(func() Ended {
			e := Event{Invocation: inv, Command: r.asked, Place: r.where(), Duration: c.Now().Sub(inv.Started), Store: r.stored, Config: r.config, Verbs: r.did, Policy: r.policy, Git: spawns(), Ending: ended}
			return Recorded{Event: e, Status: end.status}
		}, func(broke Internal) Ended {
			return Unrecorded{Said: unsaid(end, ended) + string(stackOf(end.outcome)), Broke: broke}
		})
	}, func(broke Internal) Ended { return Unrecorded{Broke: broke} })
}

// through is s's stages from Ask, each given to r as it is known: what
// was asked; what git found; then the place, held where the event reads
// it, so a place Answer moves is the place recorded; then End, over the
// answer's outcome, whatever it was.
func through[A any](r *run, s Stages[A]) cli.Outcome {
	asked, answer := s.Ask()
	r.asked = asked
	l := s.Locate()
	r.where = func() Where { return l }
	r.at = s.Identify(l)
	r.where = func() Where { return r.at }
	at := Run{r: r}
	return s.End(at, Caught(func() cli.Outcome { return s.Answer(answer, at) }, failedFor))
}

// ending is the ending of a run that said end: a failure's line handed to
// say, and what say did with it.
func ending(end said, say func(string) error) Ending {
	switch o := end.outcome.(type) {
	case cli.Done:
		return Succeeded{}
	case cli.Failed:
		return Failed{Failure: o.Failure, Line: Caught(func() Delivery {
			if err := say(end.line); err != nil {
				return Refused{Why: err}
			}
			return Delivered{}
		}, func(broke Internal) Delivery { return SayPanicked{Broke: broke} })}
	}
	panic(fmt.Sprintf("command: %T is not an Outcome", end.outcome))
}

// unsaid is end's line when Say did not deliver it, as ended says.
func unsaid(end said, ended Ending) string {
	switch e := ended.(type) {
	case Succeeded:
		return ""
	case Failed:
		switch e.Line.(type) {
		case Delivered:
			return ""
		case Refused, SayPanicked:
			return end.line
		}
		panic(fmt.Sprintf("command: %T is not a Delivery", e.Line))
	}
	panic(fmt.Sprintf("command: %T is not an Ending", ended))
}

// failedFor is the outcome of a run that broke.
func failedFor(broke Internal) cli.Outcome { return cli.Failed{Failure: broke} }

// said is an outcome with what it says: its error line and exit status.
type said struct {
	outcome cli.Outcome
	line    string
	status  int
}

// saying is o with its line and status. An Internal's are total: its
// class, reason and message are fixed when it is made, the panic's value
// formatted by fmt, which catches a panicking Error or String.
func saying(o cli.Outcome) said {
	return said{outcome: o, line: cli.Report(o), status: cli.ExitCode(o)}
}

// stackOf is the stack of the panic that ended o, when one did: an
// Internal joined with another failure keeps its stack.
func stackOf(o cli.Outcome) []byte {
	var in Internal
	if f, failed := o.(cli.Failed); failed && errors.As(f.Failure, &in) {
		return in.Stack()
	}
	return nil
}

// Caught is f's value, or, when f panics, broke's of the Internal of that
// panic, with its value and stack: the one place lit2 catches a panic, the
// caller saying what the panic means `[LAW:single-enforcer]`.
func Caught[T any](f func() T, broke func(Internal) T) (v T) {
	defer func() {
		if r := recover(); r != nil {
			v = broke(Panicked(r, debug.Stack()))
		}
	}()
	return f()
}
