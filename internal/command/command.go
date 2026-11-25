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
// trace, its release, its agent, the command it asked for, parsed or not,
// and Started, the clock's first reading, taken before the place was read,
// so the duration covers every step of the run.
type Invocation struct {
	Trace   TraceID
	Version string
	Agent   events.Agent
	Command cli.Summary
	Started time.Time
}

// Ran is what an invocation's work ended with: the place it ended in, the
// place it was handed unless admin init minted its writer, and its
// outcome.
type Ran struct {
	Place   Place
	Outcome cli.Outcome
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
	Place    Place
	Duration time.Duration
	Store    StoreFacts
	Config   *ConfigFacts
	Verbs    *VerbsFacts
	Policy   PolicyFacts
	Git      []git.Spawn
	Outcome  cli.Outcome
}

// Recorder is where a body says what it did, each fact as soon as it is
// known, so a panic keeps the facts given before it: each store step, the
// version in force, and what the verb did, the last of each kept; and the
// policy the store folded under, set as each part of it is known.
type Recorder struct {
	Stored     *StoreFacts
	Configured func(ConfigFacts)
	Told       func(VerbsFacts)
	Policy     *PolicyFacts
}

// Place is where an invocation ran.
//
//sumtype:decl
type Place interface{ place() }

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

// Record runs work at the place at as one invocation and returns its event
// on every path work takes. What was known before work runs is the
// invocation, given, not reported by work, and the place the event keeps
// is the one work ended in, returned by it `[LAW:dataflow-not-control-flow]`;
// the clock is read again only for the duration. A panic in work becomes
// an Internal outcome carrying its value and stack, and the event keeps
// what was asked and at, the place work was handed `[LAW:nothing-unseen]`.
// Each store step's result goes on Stored as
// the step returns it, so the facts of the steps that ran survive a panic
// too. The version in force arrives through Configured once the fold has
// read it, and a verb's body says what it did through Told. Every git
// process the invocation ran, before work as well as in it, is read from
// spawns once work has returned, so the event is whole when Record
// returns.
func Record(c Clock, spawns func() []git.Spawn, inv Invocation, at Place, work func(r Recorder) Ran) Event {
	var facts StoreFacts
	var config *ConfigFacts
	var did *VerbsFacts
	var policy PolicyFacts
	ran := recovered(at, func() Ran {
		return work(Recorder{
			Stored:     &facts,
			Configured: func(c ConfigFacts) { config = &c },
			Told:       func(v VerbsFacts) { did = &v },
			Policy:     &policy,
		})
	})
	return Event{Invocation: inv, Place: ran.Place, Duration: c.Now().Sub(inv.Started), Store: facts, Config: config, Verbs: did, Policy: policy, Git: spawns(), Outcome: ran.Outcome}
}

func recovered(at Place, work func() Ran) (ran Ran) {
	defer func() {
		if r := recover(); r != nil {
			ran = Ran{Place: at, Outcome: cli.Failed{Failure: Panicked(r, debug.Stack())}}
		}
	}()
	return work()
}
