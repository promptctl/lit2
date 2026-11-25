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

// Invocation is what is known of an invocation before its work runs: its
// trace, its release, its agent, and Started, the clock's first reading,
// taken before the run reads anything else, so the duration covers every
// step of it; and, once its work has said it, the command it asked for,
// parsed or not.
type Invocation struct {
	Trace   TraceID
	Version string
	Agent   events.Agent
	Command cli.Summary
	Started time.Time
}

// Ended is how an invocation ended: Recorded, its event made, or
// Unrecorded, with no event to make.
//
//sumtype:decl
type Ended interface{ ended() }

// Recorded is a run whose event was made: the event, and the error line
// and exit status of its outcome, said under the recover that made it.
type Recorded struct {
	Event  Event
	Line   string
	Status int
}

// Unrecorded is a run that made no event: a read the event is made of, at
// its start or its end, broke, Broke. Line is what the run says of its
// outcome: nothing when it broke before it had one.
type Unrecorded struct {
	Line  string
	Broke Internal
}

func (Recorded) ended()   {}
func (Unrecorded) ended() {}

// ConfigFacts is the version in force a run read from its store's fold:
// its config-ref and what it held. How many versions the log holds, and
// how many contend, are the fold's counts.
type ConfigFacts struct {
	Facts config.Facts
	// Ref is the config-ref in hex.
	Ref string
}

// Event is the command event: the invocation, how far it got in reading
// its place and, once read, the place it ended in, which says the store it
// opened, how long it took, what its store steps did, the version it ran
// under, every git process it ran, and how it ended. Config is nil when
// the run read no version, which the event records as an absent config
// block; Git is empty when no git ran.
type Event struct {
	Invocation
	Place    Where
	Duration time.Duration
	Store    StoreFacts
	Config   *ConfigFacts
	Verbs    *VerbsFacts
	Policy   PolicyFacts
	Git      []git.Spawn
	Outcome  cli.Outcome
}

// Recorder is where a run says what it did, each fact as soon as it is
// known, so a panic keeps the facts given before it. Record makes it, so
// none of its parts is missing: the facts it is told, held by Record, and
// the store steps and the policy, which each step fills in place.
type Recorder struct {
	told   *told
	Stored *StoreFacts
	Policy *PolicyFacts
}

// told is what a run told Record of itself: what was asked, how far it got
// in reading its place, the version in force, and what the verb did.
type told struct {
	asked  cli.Summary
	where  func() Where
	config *ConfigFacts
	did    *VerbsFacts
}

// Asked is what the run asked, once it is parsed.
func (r Recorder) Asked(s cli.Summary) { r.told.asked = s }

// Located is what git found where the run is, before its identity is read.
func (r Recorder) Located(l Located) { r.told.where = func() Where { return l } }

// Placed is the place the run read, held in the Here the run keeps it in:
// the one cell the event reads, so a place the body moves is the place
// recorded.
func (r Recorder) Placed(p Place) Here {
	h := Here{at: &p}
	r.told.where = func() Where { return h.Place() }
	return h
}

// Configured is the version in force, once the fold has read it.
func (r Recorder) Configured(c ConfigFacts) { r.told.config = &c }

// Told is what the verb did; the last told is kept.
func (r Recorder) Told(v VerbsFacts) { r.told.did = &v }

// Here is the place a run read and runs at: admin init's mint moves it.
type Here struct{ at *Place }

// Place is where the run is now.
func (h Here) Place() Place { return *h.at }

// Move is the run moved to p.
func (h Here) Move(p Place) { *h.at = p }

// Where is how far a run got in reading its place, as its event says it:
// PlaceUnread; a Located, the checkout found but its identity unread; or
// the Place it read. A body runs after its place is read, so its type is
// Place, and only an event holds a Where `[LAW:types-are-the-program]`.
//
//sumtype:decl
type Where interface{ where() }

// PlaceUnread is a run that ended, by a panic, before git was asked where
// it is.
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

// Record runs one invocation and returns how it ended, on every path it
// takes. begin is the reads an event is made of at its start, the trace
// and the clock; work is the rest of the run, told each fact as it learns
// it through the Recorder, so a panic keeps every fact told before it.
//
// Each read is caught, by Caught, and its panic means what its place in
// the run makes it `[LAW:nothing-unseen]`:
//   - a panic in work, or in saying its outcome's error line and status, is
//     the run's outcome, an Internal, recorded with the facts told before
//     it;
//   - a panic in begin, or in the end's reads, the clock again for the
//     duration and the git processes the run spawned, leaves no event to
//     make: the run is Unrecorded, saying the outcome's line when it had
//     one.
func Record(c Clock, spawns func() []git.Spawn, begin func() Invocation, work func(r Recorder) cli.Outcome) Ended {
	return Caught(func() Ended {
		inv := begin()
		t := &told{asked: cli.Unparsed(), where: func() Where { return PlaceUnread{} }}
		var facts StoreFacts
		var policy PolicyFacts
		end := Caught(func() said {
			return saying(work(Recorder{told: t, Stored: &facts, Policy: &policy}))
		}, func(broke Internal) said { return saying(cli.Failed{Failure: broke}) })
		return Caught(func() Ended {
			inv.Command = t.asked
			e := Event{Invocation: inv, Place: t.where(), Duration: c.Now().Sub(inv.Started), Store: facts, Config: t.config, Verbs: t.did, Policy: policy, Git: spawns(), Outcome: end.outcome}
			return Recorded{Event: e, Line: end.line, Status: end.status}
		}, func(broke Internal) Ended { return Unrecorded{Line: end.line, Broke: broke} })
	}, func(broke Internal) Ended { return Unrecorded{Broke: broke} })
}

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
