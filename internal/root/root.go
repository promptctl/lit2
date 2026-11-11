// Package root is the composition root, the one place the outside world
// enters lit2.
//
// It sits beside the stack (architecture.md §6): the one place a store is
// opened and the clock, entropy, the writer identity, the principal's key
// and the agent label are read. Main reads the world into a Process once;
// Run is the dispatcher every invocation passes through, in the order of §5.
// Tests enter through NewTwin, the same Run over a fixed clock, seeded
// entropy and a fixed writer.
package root

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/promptctl/lit2/internal/address"
	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/reason"
)

// Process is what every invocation reads from the world, read once by Main
// or set by NewTwin, every field set, so nothing downstream checks for nil.
type Process struct {
	Clock    command.Clock
	Entropy  io.Reader
	Agent    events.Agent
	Place    command.Place
	Version  string
	Commands command.Sink
	In       io.Reader
	Out, Err io.Writer
}

// Run runs one invocation: it mints the trace id, records the parse and the
// command's body as one event, puts the outcome's error line on Err, writes
// the event, and returns the outcome's exit status.
func Run(p Process, args []string) int {
	var trace command.TraceID
	if _, err := io.ReadFull(p.Entropy, trace[:]); err != nil {
		// [LAW:no-silent-failure] no trace id, no event: die loudly, as
		// crypto/rand itself does when the system's entropy fails.
		panic(fmt.Sprintf("lit2: minting the trace id: %v", err))
	}
	inv := command.Invocation{Trace: trace, Version: p.Version, Place: p.Place, Agent: p.Agent}
	event := command.Record(p.Clock, inv, func(_ time.Time, asked func(cli.Summary)) cli.Outcome {
		cmd, err := cli.Parse(cli.Verbs(), cli.Admin(), args)
		if err != nil {
			return failed(err)
		}
		asked(cli.Summarize(cmd))
		return dispatch(p, cmd)
	})
	stderr := cli.Report(event.Outcome)
	if err := p.Commands.Write(event); err != nil {
		stderr += logFailure(err)
	}
	// [LAW:no-silent-failure] exception: stderr is where failures are said;
	// when writing to it fails there is no channel left but the exit status,
	// which still carries the outcome.
	_, _ = io.WriteString(p.Err, stderr)
	return cli.ExitCode(event.Outcome)
}

// dispatch binds each command to its body.
func dispatch(p Process, cmd cli.Command) cli.Outcome {
	switch cmd.(type) {
	case cli.Help:
		return written(io.WriteString(p.Out, cli.HelpText(cli.Verbs(), cli.Admin(), address.FieldTable{})))
	case cli.Get:
		return notBuilt("get", "lit-bootstrap-1f9.7gj")
	case cli.Set:
		return notBuilt("set", "lit-bootstrap-1f9.7gj")
	case cli.Add:
		return notBuilt("add", "lit-bootstrap-1f9.7gj")
	case cli.Rm:
		return notBuilt("rm", "lit-bootstrap-1f9.7gj")
	case cli.AdminInit:
		return notBuilt("admin init", "lit-bootstrap-1f9.zc7, lit-bootstrap-1f9.m1y and lit-bootstrap-1f9.1s0")
	case cli.AdminSync:
		return notBuilt("admin sync", "lit-bootstrap-1f9.yz5")
	}
	panic(fmt.Sprintf("root: %T is not a Command", cmd))
}

// written is the outcome of writing the answer to stdout. A failed write is
// the environment's, stdout closed or full, never a lit2 bug.
func written(_ int, err error) cli.Outcome {
	if err != nil {
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
func notBuilt(verb, ticket string) cli.Outcome {
	return cli.Failed{Failure: command.Internal{Err: fmt.Errorf("lit2 %s is not built in this binary: %s", verb, ticket)}}
}
