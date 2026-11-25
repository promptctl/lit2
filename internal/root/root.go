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
	"runtime/debug"
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

// Run runs one invocation of args: it parses the command and invokes its
// answer. It puts the outcome's error line on Err before writing the
// event, so a slow or hung log cannot hold the line back, and returns the
// outcome's exit status.
func Run(p Process, args []string) int {
	asked, answer := ask(args)
	event := invoke(p, asked, answer)
	// [LAW:no-silent-failure] exception: stderr is where failures are said,
	// a lost event among them; when writing to it fails there is no channel
	// left but the exit status, which still carries the outcome.
	_, _ = io.WriteString(p.Err, cli.Report(event.Outcome))
	if err := p.Commands.Write(event); err != nil {
		_, _ = io.WriteString(p.Err, logFailure(err))
	}
	return cli.ExitCode(event.Outcome)
}

// invoke is one invocation of answer, asked as asked, the one composition
// Main's runs and the twin's, its forged writes included, share: it mints
// the trace id and reads the clock and the place, handing Record each of
// these facts as an input `[LAW:dataflow-not-control-flow]`; Record then
// records the answer, every write it made, the place it ended in and the
// temps left beside the checkout's files as one event.
func invoke(p Process, asked cli.Summary, answer func(body) command.Ran) command.Event {
	var trace command.TraceID
	if _, err := io.ReadFull(p.Entropy, trace[:]); err != nil {
		// [LAW:no-silent-failure] no trace id, no event: die loudly, as
		// crypto/rand itself does when the system's entropy fails.
		panic(fmt.Sprintf("lit2: minting the trace id: %v", err))
	}
	// read before the place, so the run's duration covers its reading
	started := p.Clock.Now()
	place := placeOf(p)
	inv := command.Invocation{Trace: trace, Version: p.Reader.String(), Agent: p.Agent, Command: asked, Started: started}
	return command.Record(p.Clock, p.Git, inv, place, func(rec command.Recorder) command.Ran {
		w := &writes{stored: rec.Stored}
		ran := unfinished(func() command.Ran {
			return answer(body{p: p, now: started, place: place, rec: rec, w: w})
		})
		ran.Outcome = w.end(ran.Outcome)
		// after the body, so the temps its creates reaped are not said
		rec.Stored.Temps = leftovers(p, ran.Place)
		return ran
	})
}

// ask is what args ask of lit2's command line, parsed or not, and its
// answer: see parsed.
func ask(args []string) (cli.Summary, func(body) command.Ran) { return parsed(cli.Lit2, args) }

// parsed is what args ask of the command line commands builds, and its
// answer: answer to the command, or the parse's refusal, one value
// whatever the parse did, so a run that parsed nothing runs the same
// steps as one that did. A line that does not build panics, a lit2 bug;
// that, or a parse that panics, is asked as unparsed and answered with
// the panic, its value and stack, as Record answers a body's, so the run
// is recorded `[LAW:nothing-unseen]`.
func parsed(commands func() cli.Commands, args []string) (asked cli.Summary, run func(body) command.Ran) {
	// [LAW:single-enforcer] exception: a second recover beside Record's,
	// since the parse runs before Record so that what was asked is its
	// input; lit-law-review-xfz.b5u makes one recover cover the whole run
	// and deletes this one.
	defer func() {
		if p := recover(); p != nil {
			broke := command.Panicked(p, debug.Stack())
			asked, run = cli.Summary{Name: commandname.Unparsed()}, func(r body) command.Ran { return r.ran(cli.Failed{Failure: broke}) }
		}
	}()
	c := commands()
	p, err := cli.Parse(c, args)
	if err != nil {
		return cli.Summary{Name: commandname.Unparsed()}, func(r body) command.Ran { return r.ran(failed(err)) }
	}
	return p.Asked, func(r body) command.Ran { return dispatch(r, c, p) }
}

// unfinished is body's outcome. A body that panics panics on, saying that
// the run's end steps, the judging of its writes' durability and the scan
// of its temps, did not run, so the event Record writes after the panic
// says so in its outcome, never an empty scan that reads as none left
// `[LAW:no-silent-failure]`.
func unfinished(body func() command.Ran) command.Ran {
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
// go, and where its writes are said.
type body struct {
	p     Process
	now   time.Time
	place command.Place
	rec   command.Recorder
	w     *writes
}

// ran is o, ended in r's place: every body's but admin init's, whose mint
// moves it.
func (r body) ran(o cli.Outcome) command.Ran { return command.Ran{Place: r.place, Outcome: o} }

// dispatch binds each command to its body. Every verb runs the steps of §5
// in order on every call: open and fold, the version in force loaded, then
// its own body. A body that names its command names it by what was
// asked, the row's name, never a spelling of its own.
func dispatch(r body, commands cli.Commands, p cli.Parsed) command.Ran {
	switch c := p.Command.(type) {
	case cli.Help:
		return r.ran(written(io.WriteString(r.p.Out, cli.HelpText(commands, r.fields()))))
	case cli.Get:
		return r.ran(r.verb(func(v inForce) cli.Outcome { return v.get(c.Paths()) }))
	case cli.Set:
		return r.ran(r.verb(func(v inForce) cli.Outcome { return v.write(verbs.Set, pairs(c.Pairs()), c.Flags()) }))
	case cli.Add:
		return r.ran(r.verb(func(v inForce) cli.Outcome { return v.write(verbs.Add, pairs(c.Pairs()), c.Flags()) }))
	case cli.Rm:
		return r.ran(r.verb(func(v inForce) cli.Outcome { return v.write(verbs.Rm, paths(c.Paths()), c.Flags()) }))
	case cli.AdminInit:
		return r.init()
	case cli.AdminSync:
		return r.ran(notBuilt(p.Asked.Name, "lit-converge-5eq"))
	case cli.ConfigCommit:
		return r.ran(r.stored(opened.commit))
	case cli.ConfigStatus:
		return r.ran(r.stored(opened.status))
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
func notBuilt(verb commandname.Name, ticket string) cli.Outcome {
	return cli.Failed{Failure: command.Broke(fmt.Errorf("%w: lit2 %s is not built in this binary; %s builds it", errNotBuilt, verb, ticket))}
}

// errNotBuilt is a row whose body this binary does not hold.
var errNotBuilt = errors.New("an unbuilt command")
