// Package root is the composition root, the one place the outside world
// enters lit2.
//
// It sits beside the stack (architecture.md §6): the one place a store is
// opened and the clock, entropy, the writer identity, the principal's key
// and the agent label are read. Main reads the world into a Process once;
// Run is the dispatcher every invocation passes through, in the order of §5.
// Tests enter through NewTwin, the same Run over a fixed clock, seeded
// entropy, a fixed writer and a store over the in-memory twins.
package root

import (
	"errors"
	"fmt"
	"io"
	"syscall"
	"time"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/commandname"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/reason"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/verbs"
)

// Process is the world every invocation reads, its ports set once by Main
// or by NewTwin, every field set, so nothing downstream checks for nil.
// Where the invocation runs, and the store it opens, are not among them:
// Run reads the place through Locate and IdentityFiles, and the store the
// place opens is opened through Stores, the same steps over the disk or
// the twins.
type Process struct {
	Clock   command.Clock
	Entropy io.Reader
	Agent   events.Agent
	// Locate is the checkout git finds where the invocation runs, none, or
	// a search that failed.
	Locate func() command.Located
	// Stores is the ports of a checkout's clone's store, every writer's.
	Stores func(git.Checkout) store.Ports
	// WorkTree is the files of a checkout's work tree's .lit directory,
	// read for workspace.toml and founding and written only by init,
	// creating them.
	WorkTree func(git.Checkout) files.Files
	// Code is a checkout's code repository, read for the blob HEAD holds
	// at a path and its history.
	Code func(git.Checkout) git.Code
	// Git is the record of each git process the invocation ran, for the
	// command's event.
	Git      func() []git.Spawn
	Fold     fold.Env
	Reader   config.ReaderVersion
	Commands command.Sink
	// IdentityFiles is a checkout's two identity directories, which admin
	// init mints into.
	IdentityFiles func(git.Checkout) (shared, private files.Files)
	// Pin is the files of a checkout's clone's founding pin, which admin
	// init writes once and every fold of the clone is seeded by.
	Pin      func(git.Checkout) files.Files
	In       io.Reader
	Out, Err io.Writer
}

// Run runs one invocation of args: it invokes what args ask of lit2's
// command line and says how it ended. A recorded run puts its outcome's
// error line on Err before writing its event, so a slow or hung log cannot
// hold the line back, and returns the outcome's exit status, unless the
// event was lost and stderr would not take the loss either. A run with no
// event says its outcome's line, when it has one, and that it went
// unrecorded, with the panic and its stack, and ends with the unrecorded
// status `[LAW:nothing-unseen]`.
func Run(p Process, args []string) int {
	// [LAW:no-silent-failure] exception: stderr is where failures are said;
	// when writing to it fails, the exit status is the one channel left. A
	// stderr that panics crashes the process, which Go ends with the
	// unrecorded status too.
	switch e := invoke(p, func() (cli.Summary, func(body) cli.Outcome) { return parsed(cli.Lit2, args) }).(type) {
	case command.Recorded:
		_, _ = io.WriteString(p.Err, e.Line)
		return logged(p, recorded(p.Commands, e.Event), e.Status)
	case command.Unrecorded:
		_, _ = io.WriteString(p.Err, e.Line+fmt.Sprintf("lit2: unrecorded: %v\n%s", e.Broke, e.Broke.Stack()))
		return cli.Unrecorded
	}
	panic("root: an Ended that is neither Recorded nor Unrecorded")
}

// recorded is e written to sink, or why it is not: a sink or an encoding
// that panicked lost the event, as a write that failed did.
func recorded(sink command.Sink, e command.Event) error {
	return command.Caught(func() error { return sink.Write(e) }, func(broke command.Internal) error { return broke })
}

// logged is the run's status once the write of its event, err, is said on
// stderr: the outcome's, unless the event was lost and stderr would not
// take the loss, when the status is the last channel left and says the
// run unrecorded. A Housekeeping is no loss, its event in the log, so its
// status is the outcome's whatever stderr took; the next write says it
// again.
func logged(p Process, err error, status int) int {
	if err == nil {
		return status
	}
	var h command.Housekeeping
	if errors.As(err, &h) {
		_, _ = io.WriteString(p.Err, "lit2: command log: "+h.Error()+"\n")
		return status
	}
	said := fmt.Sprintf("lit2: command event not recorded: %v\n", err)
	var broke command.Internal
	if errors.As(err, &broke) {
		said += string(broke.Stack())
	}
	if _, unsaid := io.WriteString(p.Err, said); unsaid != nil {
		return cli.Unrecorded
	}
	return status
}

// invoke is one invocation of what ask asks, the one composition Main's
// runs and the twin's, its forged writes included, share. It hands Record
// the reads an event is made of, the trace id and the clock, and the run:
// asking, which reads nothing, so what was asked is told before anything
// else can panic; locating the checkout, then reading its identity, each
// told as it is known; the answer, run at the place in the cell Record
// reads it from; and the end of its writes and the temps left beside the
// checkout's files.
func invoke(p Process, ask func() (cli.Summary, func(body) cli.Outcome)) command.Ended {
	var started time.Time
	begin := func() command.Invocation {
		var trace command.TraceID
		if _, err := io.ReadFull(p.Entropy, trace[:]); err != nil {
			// [LAW:no-silent-failure] no trace id, no event: the run stops
			// here and Record says it unrecorded.
			panic(fmt.Sprintf("minting the trace id: %v", err))
		}
		// read before the place, so the run's duration covers its reading
		started = p.Clock.Now()
		return command.Invocation{Trace: trace, Version: p.Reader.String(), Agent: p.Agent, Started: started}
	}
	return command.Record(p.Clock, p.Git, begin, func(rec command.Recorder) cli.Outcome {
		asked, answer := ask()
		rec.Asked(asked)
		l := p.Locate()
		rec.Located(l)
		here := rec.Placed(identified(p, l))
		w := &writes{stored: rec.Stored}
		o := w.end(unfinished(func() cli.Outcome {
			return answer(body{p: p, now: started, place: here, rec: rec, w: w})
		}))
		// after the body, so the temps its creates reaped are not said
		rec.Stored.Temps = leftovers(p, here.Place())
		return o
	})
}

// parsed is what args ask of the command line commands builds, and its
// answer: answer to the command, or the parse's refusal, one value
// whatever the parse did, so a run that parsed nothing runs the same
// steps as one that did. A line that does not build panics, a lit2 bug,
// recorded as any panic in a run is.
func parsed(commands func() cli.Commands, args []string) (cli.Summary, func(body) cli.Outcome) {
	c := commands()
	p, err := cli.Parse(c, args)
	if err != nil {
		return cli.Unparsed(), func(body) cli.Outcome { return failed(err) }
	}
	return p.Asked, func(r body) cli.Outcome { return dispatch(r, c, p) }
}

// unfinished is body's outcome. A body that panics panics on, saying that
// the run's end steps, the judging of its writes' durability and the scan
// of its temps, did not run, so the event Record writes after the panic
// says so in its outcome, never an empty scan that reads as none left
// `[LAW:no-silent-failure]`.
func unfinished(body func() cli.Outcome) cli.Outcome {
	defer func() {
		if r := recover(); r != nil {
			panic(stoppedBeforeItsEnd{panicked: r})
		}
	}()
	return body()
}

// stoppedBeforeItsEnd is a body's panic, said with the end steps it kept
// from running.
type stoppedBeforeItsEnd struct{ panicked any }

func (s stoppedBeforeItsEnd) String() string {
	return fmt.Sprintf("%v; the run stopped before its end: the durability of its writes was not judged, and no directory's temps were scanned", s.panicked)
}

// body is one invocation as its body sees it: the process, the clock's
// first reading, its place, which says the store it opens, where its facts
// go, and where its writes are said. The place is Here, held where Record
// reads it, so admin init, whose mint moves it, moves the place recorded.
type body struct {
	p     Process
	now   time.Time
	place command.Here
	rec   command.Recorder
	w     *writes
}

// dispatch binds each command to its body. Every verb runs the steps of §5
// in order on every call: open and fold, the version in force loaded, then
// its own body. Only the unbuilt row names the command it runs, and by
// what was asked; a message telling a person which command to run next
// reads commandname.
func dispatch(r body, commands cli.Commands, p cli.Parsed) cli.Outcome {
	switch c := p.Command.(type) {
	case cli.Help:
		return written(io.WriteString(r.p.Out, cli.HelpText(commands, r.fields())))
	case cli.Get:
		return r.verb(func(v inForce) cli.Outcome { return v.get(c.Paths()) })
	case cli.Set:
		return r.verb(func(v inForce) cli.Outcome { return v.write(verbs.Set, pairs(c.Pairs()), c.Flags()) })
	case cli.Add:
		return r.verb(func(v inForce) cli.Outcome { return v.write(verbs.Add, pairs(c.Pairs()), c.Flags()) })
	case cli.Rm:
		return r.verb(func(v inForce) cli.Outcome { return v.write(verbs.Rm, paths(c.Paths()), c.Flags()) })
	case cli.Init:
		return r.init()
	case cli.Sync:
		return notBuilt(p.Asked.Name, "lit-converge-5eq")
	case cli.Commit:
		return r.stored(opened.commit)
	case cli.Status:
		return r.stored(opened.status)
	}
	panic(fmt.Sprintf("root: %T is not a Command", p.Command))
}

// written is the outcome of writing the answer to stdout. EPIPE is the
// reader leaving, as `lit2 ... | head` does: lit2 did what was asked, so it
// is Done, silent and 0, as conventional tools are. Any other failed write
// is the environment's, stdout closed or full, never a lit2 bug.
func written(_ int, err error) cli.Outcome {
	if err != nil && !errors.Is(err, syscall.EPIPE) {
		return cli.Failed{Failure: output{err: err}}
	}
	return cli.Done{}
}

// output is an answer that could not be written to stdout.
type output struct{ err error }

func (o output) Error() string     { return "writing the answer to stdout: " + o.err.Error() }
func (o output) Unwrap() error     { return o.err }
func (output) Class() reason.Class { return reason.ClassEnvironment }
func (output) Reason() string      { return "output" }

// failed is an error as an outcome: a Failure is its own class, and any
// other error is a lit2 bug, reported as one `[LAW:no-silent-failure]`.
func failed(err error) cli.Outcome {
	var f reason.Failure
	if errors.As(err, &f) {
		return cli.Failed{Failure: f}
	}
	return cli.Failed{Failure: command.Broke(err)}
}

// notBuilt ends a row whose body is not in this binary yet, naming ticket,
// the ticket that builds it (D14). Once the last body is bound it is
// unused, and the lint forces its deletion.
func notBuilt(verb commandname.Name, ticket string) cli.Outcome {
	return cli.Failed{Failure: command.Broke(fmt.Errorf("%w: lit2 %s is not built in this binary; %s builds it", errNotBuilt, verb, ticket))}
}

// errNotBuilt is a row whose body this binary does not hold.
var errNotBuilt = errors.New("an unbuilt command")
