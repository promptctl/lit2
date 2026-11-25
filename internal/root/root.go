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
// Where the invocation runs, and the store it holds, are not among them:
// Run reads the place through Locate and IdentityFiles and opens its store
// through Stores, the same steps over the disk or the twins.
type Process struct {
	Clock   command.Clock
	Entropy io.Reader
	Agent   events.Agent
	// Locate is the directory the invocation runs in and the checkout git
	// finds there: git.NotFound when there is none, and any other error
	// when the search itself failed.
	Locate func() (dir string, c git.Checkout, err error)
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

// Run runs one invocation, the one composition Main and the twin share:
// it mints the trace id, reads the clock, the place and the store that
// place opens, and parses the command, handing Record each of these facts
// as an input `[LAW:dataflow-not-control-flow]`; Record then records the
// command's body, every write it made and the temps left beside the
// checkout's files as one event. It puts the outcome's error line on
// Err before writing the event, so a slow or hung log cannot hold the line
// back, and returns the outcome's exit status.
func Run(p Process, args []string) int {
	var trace command.TraceID
	if _, err := io.ReadFull(p.Entropy, trace[:]); err != nil {
		// [LAW:no-silent-failure] no trace id, no event: die loudly, as
		// crypto/rand itself does when the system's entropy fails.
		panic(fmt.Sprintf("lit2: minting the trace id: %v", err))
	}
	// read before the place, so the run's duration covers its reading
	started := p.Clock.Now()
	place := placeOf(p)
	at := situate(p, place)
	table, groups := cli.Verbs(), cli.Groups()
	cmd, unparsed := cli.Parse(table, groups, args)
	asked := cli.Summary{Name: cli.Unparsed}
	if unparsed == nil {
		asked = cli.Summarize(cmd)
	}
	inv := command.Invocation{Trace: trace, Version: p.Reader.String(), Place: place, Agent: p.Agent}
	event := command.Record(p.Clock, p.Git, inv, asked, started, func(rec command.Recorder) cli.Outcome {
		if unparsed != nil {
			return failed(unparsed)
		}
		w := &writes{stored: rec.Stored}
		out := w.end(unfinished(func() cli.Outcome {
			return dispatch(body{p: p, now: started, place: place, at: at, rec: rec, w: w}, table, groups, cmd)
		}))
		// after the body, so the temps its creates reaped are not said
		rec.Stored.Temps = leftovers(p, place)
		return out
	})
	// [LAW:no-silent-failure] exception: stderr is where failures are said,
	// a lost event among them; when writing to it fails there is no channel
	// left but the exit status, which still carries the outcome.
	_, _ = io.WriteString(p.Err, cli.Report(event.Outcome))
	if err := p.Commands.Write(event); err != nil {
		_, _ = io.WriteString(p.Err, logFailure(err))
	}
	return cli.ExitCode(event.Outcome)
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
// first reading, its place and the store that place holds, where its facts
// go, and where its writes are said.
type body struct {
	p     Process
	now   time.Time
	place command.Place
	at    situation
	rec   command.Recorder
	w     *writes
}

// dispatch binds each command to its body. Every verb runs the steps of §5
// in order on every call: open and fold, the version in force loaded, then
// its own body.
func dispatch(r body, table cli.Table, groups []cli.Group, cmd cli.Command) cli.Outcome {
	switch c := cmd.(type) {
	case cli.Help:
		return written(io.WriteString(r.p.Out, cli.HelpText(table, groups, r.fields())))
	case cli.Get:
		return r.verb(func(v inForce) cli.Outcome { return v.get(c.Paths()) })
	case cli.Set:
		return r.verb(func(v inForce) cli.Outcome { return v.write(verbs.Set, pairs(c.Pairs()), c.Flags()) })
	case cli.Add:
		return r.verb(func(v inForce) cli.Outcome { return v.write(verbs.Add, pairs(c.Pairs()), c.Flags()) })
	case cli.Rm:
		return r.verb(func(v inForce) cli.Outcome { return v.write(verbs.Rm, paths(c.Paths()), c.Flags()) })
	case cli.AdminInit:
		return r.init()
	case cli.AdminSync:
		return notBuilt("admin sync", "lit-converge-5eq")
	case cli.ConfigCommit:
		return r.stored(opened.commit)
	case cli.ConfigStatus:
		return r.stored(opened.status)
	}
	panic(fmt.Sprintf("root: %T is not a Command", cmd))
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

// logFailure is what a failed command log write says on stderr: an event
// whose old day files could not be removed is in the log, and any other
// failure means it is not.
func logFailure(err error) string {
	var h command.Housekeeping
	if errors.As(err, &h) {
		return fmt.Sprintf("lit2: command log: pruning failed: %v\n", h)
	}
	return fmt.Sprintf("lit2: command event not recorded: %v\n", err)
}

// notBuilt ends a row whose body is not in this binary yet, naming ticket,
// the ticket that builds it (D14). Once the last body is bound it is
// unused, and the lint forces its deletion.
func notBuilt(verb, ticket string) cli.Outcome {
	return cli.Failed{Failure: command.Broke(fmt.Errorf("%w: lit2 %s is not built in this binary; %s builds it", errNotBuilt, verb, ticket))}
}

// errNotBuilt is a row whose body this binary does not hold.
var errNotBuilt = errors.New("an unbuilt command")
