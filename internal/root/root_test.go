package root

import (
	"errors"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
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

func reasonOf(e command.Event) string {
	failed, ok := e.Outcome.(cli.Failed)
	if !ok {
		return ""
	}
	return failed.Failure.Reason()
}

func TestHelpPrintsTheVerbsAndExitsZero(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {}} {
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
	status, e := run(t, twin, "get", "x")
	if status != 70 || reasonOf(e) != "internal" || !strings.Contains(twin.Err.String(), "lit2 get is not built in this binary") {
		t.Errorf("lit2 get x: status %d, stderr %q", status, twin.Err)
	}
	if e.Command.Name != "get" || len(e.Command.Paths) != 1 {
		t.Errorf("lit2 get x recorded %#v; want get with its one path", e.Command)
	}
}

type brokenPipe struct{}

func (brokenPipe) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

func TestAnErrorThatIsNotAFailureIsInternal(t *testing.T) {
	twin := NewTwin()
	twin.Process.Out = brokenPipe{}
	status, e := run(t, twin, "--help")
	if status != 70 || reasonOf(e) != "internal" || !strings.Contains(twin.Err.String(), "broken pipe") {
		t.Errorf("--help into a broken pipe: status %d, stderr %q", status, twin.Err)
	}
}

func TestTheEventIsInTheTwinsCheckoutAsItsWriter(t *testing.T) {
	twin := NewTwin()
	_, e := run(t, twin, "--help")
	want := command.InCheckout{Checkout: twin.Workspace.Checkout, Writer: command.Minted{ID: twin.Workspace.Writer}}
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
