// Package command is the command event, the one record each invocation
// leaves in this machine's command log.
//
// It sits beside the stack (architecture.md §6) and is §5 step 8, Record:
// one wide event per invocation, local, never synced `[LAW:nothing-unseen]`.
// Record wraps every invocation's work, so an invocation with no event is
// unrepresentable; each later step adds its facts to the event as it lands.
package command

import (
	"encoding/hex"
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

// Invocation is what is known of an invocation: its trace, its release,
// its agent, the command it asked for, parsed or not, and Started, the
// clock's first reading, taken before anything else the run does, so the
// duration covers every step of it. All but the command are known before
// its work runs; the command is what its work says was asked.
type Invocation struct {
	Trace   TraceID
	Version string
	Agent   events.Agent
	Command cli.Summary
	Started time.Time
}

// Ended is an invocation's end: its event, and the error line and exit
// status of its outcome, derived once, under the recover that made the
// outcome, so nothing the process does after reads the outcome again.
type Ended struct {
	Event  Event
	Line   string
	Status int
}

// ConfigFacts is the version in force a run read from its store's fold:
// its config-ref and what it held. How many versions the log holds, and
// how many contend, are the fold's counts.
type ConfigFacts struct {
	Facts config.Facts
	// Ref is the config-ref in hex.
	Ref string
}

// Event is the command event: the invocation, the place it ended in, which
// says the store it opened, how long it took, what its store steps did,
// the version it ran under, every git process it ran, and how it ended.
// Config is nil when the run read no version, which the event records as
// an absent config block; Git is empty when no git ran.
type Event struct {
	Invocation
	Place    Placed
	Duration time.Duration
	Store    StoreFacts
	Config   *ConfigFacts
	Verbs    *VerbsFacts
	Policy   PolicyFacts
	Git      []git.Spawn
	Outcome  cli.Outcome
}

// Recorder is where a run says what it did, each fact as soon as it is
// known, so a panic keeps the facts given before it: what was asked; the
// place, as the cell the body reads and moves it in, so the event reads
// the one place the body held; each store step, the version in force, and
// what the verb did, the last of each kept; and the policy the store
// folded under, set as each part of it is known.
type Recorder struct {
	Asked      func(cli.Summary)
	Placed     func(*Place)
	Stored     *StoreFacts
	Configured func(ConfigFacts)
	Told       func(VerbsFacts)
	Policy     *PolicyFacts
}

// Place is where an invocation ran.
//
//sumtype:decl
type Place interface {
	Placed
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

// Placed is where an event says its run was: the Place it read, or
// PlaceUnread. Only an event holds one; a body runs after its place is
// read, so its type is Place `[LAW:types-are-the-program]`.
//
//sumtype:decl
type Placed interface{ placed() }

// PlaceUnread is a run that ended, by a panic, before its place was read.
type PlaceUnread struct{}

func (InCheckout) placed()  {}
func (NoCheckout) placed()  {}
func (Unlocatable) placed() {}
func (PlaceUnread) placed() {}

// Located is what git found where an invocation runs, before its identity
// is read: a checkout, none, or a search that failed.
//
//sumtype:decl
type Located interface{ located() }

// AtCheckout is a checkout git found.
type AtCheckout struct{ Checkout git.Checkout }

func (AtCheckout) located()  {}
func (NoCheckout) located()  {}
func (Unlocatable) located() {}

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
// taken.
func Panicked(value any, stack []byte) Internal {
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

// Record runs work as one invocation and returns its end on every path
// work takes: the event, and the outcome's error line and exit status,
// derived inside the one recover, so a panic in any step of work, or in
// rendering what its outcome says, is an Internal outcome carrying its
// value and stack `[LAW:single-enforcer]` `[LAW:nothing-unseen]`. What was
// known before work runs is inv; what work learns goes on the Recorder as
// it learns it, so a panic keeps every fact given before it: what was
// asked, else inv's command; the place, else PlaceUnread, read from the
// cell the body holds it in, so a place a body moved is the one recorded;
// each store step's result, the version in force and what a verb did. The
// clock is read again only for the duration, and every git process the
// invocation ran is read from spawns once work has returned, so the event
// is whole when Record returns. Those two reads are outside the recover: a
// panic there leaves no event, and its caller says the run unrecorded.
func Record(c Clock, spawns func() []git.Spawn, inv Invocation, work func(r Recorder) cli.Outcome) Ended {
	var at *Place
	var facts StoreFacts
	var config *ConfigFacts
	var did *VerbsFacts
	var policy PolicyFacts
	var line string
	var status int
	outcome, broke, panicked := Caught(func() cli.Outcome {
		o := work(Recorder{
			Asked:      func(s cli.Summary) { inv.Command = s },
			Placed:     func(p *Place) { at = p },
			Stored:     &facts,
			Configured: func(c ConfigFacts) { config = &c },
			Told:       func(v VerbsFacts) { did = &v },
			Policy:     &policy,
		})
		line, status = cli.Report(o), cli.ExitCode(o)
		return o
	})
	if panicked {
		// total: an Internal's class, reason and message are fixed when it
		// is made, the panic's value formatted by fmt, which catches a
		// panicking Error or String
		outcome = cli.Failed{Failure: broke}
		line, status = cli.Report(outcome), cli.ExitCode(outcome)
	}
	var place Placed = PlaceUnread{}
	if at != nil {
		place = *at
	}
	e := Event{Invocation: inv, Place: place, Duration: c.Now().Sub(inv.Started), Store: facts, Config: config, Verbs: did, Policy: policy, Git: spawns(), Outcome: outcome}
	return Ended{Event: e, Line: line, Status: status}
}

// Caught is f's value, or the Internal of its panic, with its value and
// stack: the one place a panic is caught, each caller saying what its
// panic means for the run.
func Caught[T any](f func() T) (v T, broke Internal, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			broke, panicked = Panicked(r, debug.Stack()), true
		}
	}()
	return f(), Internal{}, false
}
