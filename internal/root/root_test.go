package root

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/reason"
)

// run runs one invocation on a fresh twin and returns its status and the
// one event it left, whose exit code must be that status.
func run(t *testing.T, twin Twin, args ...string) (int, command.Event) {
	t.Helper()
	status := Run(twin.Process, args)
	if len(twin.Commands.Events) != 1 {
		t.Fatalf("lit2 %q left %d events; want 1", args, len(twin.Commands.Events))
	}
	e := twin.Commands.Events[0]
	if got := cli.ExitCode(e.Outcome); got != status {
		t.Errorf("lit2 %q exited %d but its event records %d", args, status, got)
	}
	return status, e
}

// failure is the failure e records, failing t when e records none.
func failure(t *testing.T, e command.Event) reason.Failure {
	t.Helper()
	failed, ok := e.Outcome.(cli.Failed)
	if !ok {
		t.Fatalf("the run ended %#v; want it failed", e.Outcome)
	}
	return failed.Failure
}

func reasonOf(e command.Event) string {
	failed, ok := e.Outcome.(cli.Failed)
	if !ok {
		return ""
	}
	return failed.Failure.Reason()
}

func TestHelpPrintsTheVerbsAndExitsZero(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"get", "--help"}} {
		twin := NewTwin()
		status, e := run(t, twin, args...)
		for _, verb := range []string{"get", "set", "add", "rm", "admin init", "admin sync"} {
			if !strings.Contains(twin.Out.String(), verb) {
				t.Errorf("lit2 %q does not name %s on stdout", args, verb)
			}
		}
		if status != 0 || twin.Err.Len() != 0 || e.Command.Name != "help" {
			t.Errorf("lit2 %q: status %d, stderr %q, command %q; want 0, nothing, help", args, status, twin.Err, e.Command.Name)
		}
	}
}

func TestAnUnknownVerbIsUsage(t *testing.T) {
	twin := NewTwin()
	status, e := run(t, twin, "frobnicate")
	if status != 64 || !strings.HasPrefix(twin.Err.String(), "error (reason=unknown_verb): ") || reasonOf(e) != "unknown_verb" || e.Command.Name != cli.Unparsed {
		t.Errorf("lit2 frobnicate: status %d, stderr %q, event %#v", status, twin.Err, e)
	}
	if twin.Out.Len() != 0 {
		t.Errorf("lit2 frobnicate printed %q on stdout", twin.Out)
	}
}

func TestARowWithNoBodyIsInternal(t *testing.T) {
	twin := NewTwin()
	status, e := run(t, twin, "admin", "sync")
	if status != 70 || !errors.Is(failure(t, e), errNotBuilt) || !strings.Contains(twin.Err.String(), "lit2 admin sync is not built in this binary; lit-converge-5eq builds it") {
		t.Errorf("lit2 admin sync: status %d, stderr %q", status, twin.Err)
	}
	if e.Command.Name != "admin sync" {
		t.Errorf("lit2 admin sync recorded %#v", e.Command)
	}
}

type refusing struct{ err error }

func (r refusing) Write([]byte) (int, error) { return 0, r.err }

func TestAnAnswerStdoutRefusesIsTheEnvironments(t *testing.T) {
	twin := NewTwin()
	twin.Process.Out = refusing{fmt.Errorf("write /dev/stdout: %w", syscall.ENOSPC)}
	status, e := run(t, twin, "--help")
	var out output
	if status != 78 || !errors.As(failure(t, e), &out) || !errors.Is(out, syscall.ENOSPC) || !strings.Contains(twin.Err.String(), "no space left") {
		t.Errorf("--help into a full stdout: status %d, stderr %q", status, twin.Err)
	}
}

func TestAReaderLeavingIsDone(t *testing.T) {
	twin := NewTwin()
	twin.Process.Out = refusing{fmt.Errorf("write /dev/stdout: %w", syscall.EPIPE)}
	if status, e := run(t, twin, "--help"); status != 0 || e.Outcome != (cli.Done{}) || twin.Err.Len() != 0 {
		t.Errorf("--help into a pipe whose reader left: status %d, outcome %#v, stderr %q", status, e.Outcome, twin.Err)
	}
}

// peek is a command log that keeps what stderr held when the event came.
type peek struct {
	stderr *bytes.Buffer
	saw    string
}

func (p *peek) Write(command.Event) error {
	p.saw = p.stderr.String()
	return nil
}

func TestTheOutcomeIsSaidBeforeTheEventIsWritten(t *testing.T) {
	twin := NewTwin()
	log := &peek{stderr: twin.Err}
	twin.Process.Commands = log
	Run(twin.Process, []string{"frobnicate"})
	if !strings.HasPrefix(log.saw, "error (reason=unknown_verb): ") {
		t.Errorf("stderr held %q when the event was written; want the outcome's line", log.saw)
	}
}

func TestAnErrorThatIsNotAFailureIsInternal(t *testing.T) {
	o := failed(errors.New("an unclassified error"))
	if f, ok := o.(cli.Failed); !ok || cli.ExitCode(o) != 70 || f.Failure.Reason() != "internal" {
		t.Errorf("an unclassified error is %#v; want internal, exit 70", o)
	}
}

func TestTheEventIsInTheTwinsCheckoutAsItsWriter(t *testing.T) {
	twin := NewTwin()
	_, e := run(t, twin, "--help")
	want := command.InCheckout{Checkout: twinCheckout, Identity: command.Minted{Checkout: twinID, Key: twinKey()}}
	if e.Place != want {
		t.Errorf("the event's place is %#v; want %#v", e.Place, want)
	}
}

func TestEachInvocationHasItsOwnTrace(t *testing.T) {
	twin := NewTwin()
	Run(twin.Process, []string{"--help"})
	Run(twin.Process, []string{"--help"})
	if events := twin.Commands.Events; len(events) != 2 || events[0].Trace == events[1].Trace {
		t.Errorf("two invocations share a trace id: %#v", events)
	}
}
