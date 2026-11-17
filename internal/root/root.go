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
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/reason"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/verbs"
)

// Process is what every invocation reads from the world, read once by Main
// or set by NewTwin, every field set, so nothing downstream checks for nil.
type Process struct {
	Clock    command.Clock
	Entropy  io.Reader
	Agent    events.Agent
	Place    command.Place
	Store    command.Opening
	Fold     fold.Env
	Reader   config.ReaderVersion
	Commands command.Sink
	// IdentityFiles is a checkout's two identity directories, which admin
	// init mints into.
	IdentityFiles func(git.Checkout) (shared, private store.Files)
	In            io.Reader
	Out, Err      io.Writer
}

// Run runs one invocation: it mints the trace id, records the store's
// opening, the parse and the command's body as one event, puts the outcome's error line on Err before
// writing the event, so a slow or hung log cannot hold the line back, and
// returns the outcome's exit status.
func Run(p Process, args []string) int {
	var trace command.TraceID
	if _, err := io.ReadFull(p.Entropy, trace[:]); err != nil {
		// [LAW:no-silent-failure] no trace id, no event: die loudly, as
		// crypto/rand itself does when the system's entropy fails.
		panic(fmt.Sprintf("lit2: minting the trace id: %v", err))
	}
	table, admin := cli.Verbs(), cli.Admin()
	loaded := load(p)
	inv := command.Invocation{Trace: trace, Version: p.Reader.String(), Place: p.Place, Agent: p.Agent, Config: configFacts(p, loaded)}
	event := command.Record(p.Clock, inv, func(now time.Time, asked func(cli.Summary), stored *command.StoreFacts, told func(command.VerbsFacts)) cli.Outcome {
		stored.Opening = p.Store
		cmd, err := cli.Parse(table, admin, args)
		if err != nil {
			return failed(err)
		}
		asked(cli.Summarize(cmd))
		return dispatch(body{p: p, loaded: loaded, now: now, stored: stored, told: told}, table, admin, cmd)
	})
	// [LAW:no-silent-failure] exception: stderr is where failures are said;
	// when writing to it fails there is no channel left but the exit status,
	// which still carries the outcome.
	_, _ = io.WriteString(p.Err, cli.Report(event.Outcome))
	if err := p.Commands.Write(event); err != nil {
		_, _ = io.WriteString(p.Err, logFailure(err))
	}
	return cli.ExitCode(event.Outcome)
}

// load reads the workspace configuration in force for this run, once. Only a
// committed version is in force (architecture.md §3.15), and none is committed
// yet, so the shipped default is the model. Loading reads nothing from the
// working tree, so a draft that does not parse never reaches a command.
func load(p Process) config.Loaded {
	loaded, err := config.Load(config.NeverCommitted{}, p.Reader)
	if err != nil {
		// [LAW:no-silent-failure] the default profile is embedded in this
		// binary; one that does not parse is a defect of the build, not a run
		// to refuse.
		panic(fmt.Sprintf("root: the shipped default profile does not parse: %v", err))
	}
	return loaded
}

// configFacts is the configuration the run read. The config-ref names the
// version in the code repository's object format, which only an open store
// has, so a run with no store records no ref.
func configFacts(p Process, loaded config.Loaded) *command.ConfigFacts {
	f := command.ConfigFacts{Facts: loaded.Facts()}
	if o, ok := p.Store.(command.Opened); ok {
		f.Ref = loaded.Ref(o.Format).String()
	}
	return &f
}

// body is one invocation as its body sees it: the process, the
// configuration loaded once, the clock's one reading, and where its facts
// go.
type body struct {
	p      Process
	loaded config.Loaded
	now    time.Time
	stored *command.StoreFacts
	told   func(command.VerbsFacts)
}

// dispatch binds each command to its body. Every verb runs the steps of §5
// in order on every call: open and fold, then its own body.
func dispatch(r body, table, admin cli.Table, cmd cli.Command) cli.Outcome {
	switch c := cmd.(type) {
	case cli.Help:
		return written(io.WriteString(r.p.Out, cli.HelpText(table, admin, r.loaded.Schema().Table())))
	case cli.Get:
		return verb(r.p, r.stored, func(f fold.Folded) cli.Outcome { return r.get(f, c.Paths()) })
	case cli.Set:
		return verb(r.p, r.stored, func(f fold.Folded) cli.Outcome { return r.write(f, verbs.Set, pairs(c.Pairs()), c.Flags()) })
	case cli.Add:
		return verb(r.p, r.stored, func(f fold.Folded) cli.Outcome { return r.write(f, verbs.Add, pairs(c.Pairs()), c.Flags()) })
	case cli.Rm:
		return verb(r.p, r.stored, func(f fold.Folded) cli.Outcome { return r.write(f, verbs.Rm, paths(c.Paths()), c.Flags()) })
	case cli.AdminInit:
		return r.init()
	case cli.AdminSync:
		return notBuilt("admin sync")
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
func (output) Class() reason.Class { return reason.ClassEnvironment }
func (output) Reason() string      { return "output" }

// failed is an error as an outcome: a Failure is its own class, and any
// other error is a lit2 bug, reported as one `[LAW:no-silent-failure]`.
func failed(err error) cli.Outcome {
	var f reason.Failure
	if errors.As(err, &f) {
		return cli.Failed{Failure: f}
	}
	return cli.Failed{Failure: command.Internal{Err: err}}
}

// logFailure is what a failed command log write says on stderr: an event
// whose old day files could not be removed is in the log, and any other
// failure means it is not.
func logFailure(err error) string {
	var h command.Housekeeping
	if errors.As(err, &h) {
		return fmt.Sprintf("lit2: command log: pruning failed: %v\n", h.Err)
	}
	return fmt.Sprintf("lit2: command event not recorded: %v\n", err)
}

// notBuilt ends a row whose body is not in this binary yet (D14). Once the
// last body is bound it is unused, and the lint forces its deletion.
func notBuilt(verb string) cli.Outcome {
	return cli.Failed{Failure: command.Internal{Err: fmt.Errorf("lit2 %s is not built in this binary", verb)}}
}
