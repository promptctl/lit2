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
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/reason"
	"github.com/promptctl/lit2/internal/store"
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

// Invocation is what is known of an invocation before its work runs.
type Invocation struct {
	Trace   TraceID
	Version string
	Place   Place
	Agent   events.Agent
}

// Event is the command event: the invocation, what was asked, when it
// started, how long it took, what its store steps did, and how it ended.
type Event struct {
	Invocation
	Command  cli.Summary
	Started  time.Time
	Duration time.Duration
	Store    StoreFacts
	Outcome  cli.Outcome
}

// Place is where an invocation ran.
//
//sumtype:decl
type Place interface{ place() }

// InCheckout is an invocation inside a git checkout, with its writer.
type InCheckout struct {
	Checkout git.Checkout
	Writer   Writer
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

// Writer is a checkout's writer identity, read from its id file.
//
//sumtype:decl
type Writer interface{ writer() }

// Minted is a checkout whose id file holds its id.
type Minted struct{ ID store.CheckoutID }

// Unminted is a checkout with no id file: lit2 admin init has not run.
type Unminted struct{ Path string }

// Unreadable is an id file that is present and cannot be read as an id,
// which is not the same fact as no id file.
type Unreadable struct {
	Path string
	Err  error
}

func (Minted) writer()     {}
func (Unminted) writer()   {}
func (Unreadable) writer() {}

// Internal is lit2 failing: an error that answers no class, a panic, a row
// whose body is not built. Stack is the panic's stack, empty otherwise.
type Internal struct {
	Err   error
	Stack []byte
}

func (i Internal) Error() string { return i.Err.Error() }

// Class is internal.
func (Internal) Class() reason.Class { return reason.ClassInternal }

// Reason is internal.
func (Internal) Reason() string { return "internal" }

// Record runs work as one invocation and returns its event on every path.
// The first clock reading is now, handed to work and recorded as Started;
// the second is only the duration. What was asked is Unparsed until work
// says otherwise through asked, as soon as it knows, before its body runs,
// so the event keeps the last summary given even when the body panics
// `[LAW:nothing-unseen]`; the panic becomes an Internal outcome carrying
// its value and stack. Each store step's result goes on stored as the step
// returns it, so the facts of the steps that ran survive a panic too.
func Record(c Clock, inv Invocation, work func(now time.Time, asked func(cli.Summary), stored *StoreFacts) cli.Outcome) Event {
	started := c.Now()
	summary := cli.Summary{Name: cli.Unparsed}
	var facts StoreFacts
	outcome := recovered(func() cli.Outcome { return work(started, func(s cli.Summary) { summary = s }, &facts) })
	return Event{Invocation: inv, Command: summary, Started: started, Duration: c.Now().Sub(started), Store: facts, Outcome: outcome}
}

func recovered(work func() cli.Outcome) (outcome cli.Outcome) {
	defer func() {
		if r := recover(); r != nil {
			outcome = cli.Failed{Failure: Internal{Err: fmt.Errorf("panic: %v", r), Stack: debug.Stack()}}
		}
	}()
	return work()
}
