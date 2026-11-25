package root

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/commandname"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/reason"
)

// asking is what Run asks of lit2's command line built by commands.
func asking(commands func() cli.Commands, args ...string) func() (cli.Summary, func(body) cli.Outcome) {
	return func() (cli.Summary, func(body) cli.Outcome) { return parsed(commands, args) }
}

// A panic in any stage between the run's start and its event is recorded
// as lit2's failure, its value and stack kept, with what was known before
// it: what was asked, once asking ended, and the place, once it was read.
func TestAPanicInAnyStageIsRecorded(t *testing.T) {
	for _, tc := range []struct {
		stage  string
		twin   func(Twin) Twin
		ask    func() (cli.Summary, func(body) cli.Outcome)
		asked  commandname.Name
		placed bool
	}{
		{"building the command table", func(t Twin) Twin { return t }, asking(func() cli.Commands { panic("the table broke") }, "get", "x"), commandname.Unparsed(), false},
		{"reading the place", func(t Twin) Twin {
			t.Process.Locate = func() command.Located { panic("the place broke") }
			return t
		}, asking(cli.Lit2, "get", "x"), commandname.Get(), false},
		{"running the command", func(t Twin) Twin {
			t.Process.Out = panicking{}
			return t
		}, asking(cli.Lit2, "--help"), commandname.Help(), true},
	} {
		ended := invoke(tc.twin(NewTwin()).Process, tc.ask)
		e := ended.Event
		var in command.Internal
		if !errors.As(failure(t, e), &in) || !strings.Contains(in.Error(), "broke") || len(in.Stack()) == 0 || ended.Status != 70 {
			t.Errorf("%s: ended %#v; want Internal with its value and stack, exit 70", tc.stage, e.Outcome)
		}
		if _, read := e.Place.(command.Place); e.Command.Name != tc.asked || read != tc.placed {
			t.Errorf("%s: recorded %s at %#v; want %s, the place read: %v", tc.stage, e.Command.Name, e.Place, tc.asked, tc.placed)
		}
	}
}

// panicking is a stdout whose writes panic.
type panicking struct{}

func (panicking) Write([]byte) (int, error) { panic("stdout broke") }

// A panic after admin init's mint is recorded in the minted place: the
// place the body moved is the place the event reads.
func TestAPanicAfterTheMintIsRecordedMinted(t *testing.T) {
	twin := withIDFile(t, NewFreshTwin(), nil)
	pins := twin.Process.Pin
	calls := 0
	twin.Process.Pin = func(c git.Checkout) files.Files {
		// the first read is init's, of its once files, before the mint
		if calls++; calls > 1 {
			panic("the pin broke after the mint")
		}
		return pins(c)
	}
	status, e := run(t, twin, commandname.Init().Words()...)
	in, _ := readPlace(t, e).(command.InCheckout)
	if _, minted := in.Identity.(command.Minted); status != 70 || !minted {
		t.Errorf("init panicking after its mint exited %d at %#v; want 70, at the minted checkout", status, e.Place)
	}
}

// A run whose start panics, its trace id or its clock, has no event: it
// says so on stderr and ends unrecorded.
func TestAStartThatFailsIsSaidUnrecorded(t *testing.T) {
	for name, broke := range map[string]func(Twin) Twin{
		"the trace id": func(t Twin) Twin {
			t.Process.Entropy = refusingReader{}
			return t
		},
		"the clock": func(t Twin) Twin {
			t.Process.Clock = panickingClock{}
			return t
		},
	} {
		twin := broke(NewTwin())
		status := Run(twin.Process, []string{"--help"})
		if status != cli.Unrecorded || len(twin.Commands.Events) != 0 || !strings.HasPrefix(twin.Err.String(), "lit2: unrecorded: ") {
			t.Errorf("%s failing: status %d, %d events, stderr %q; want %d, none, the unrecorded line", name, status, len(twin.Commands.Events), twin.Err, cli.Unrecorded)
		}
	}
}

type refusingReader struct{}

func (refusingReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

type panickingClock struct{}

func (panickingClock) Now() time.Time { panic("the clock broke") }

// A sink that fails or panics loses the event, said on stderr; when stderr
// refuses that too, the status says the run unrecorded. A log whose
// pruning did not finish kept the event, so the status is the outcome's.
func TestALostEventNeverLooksClean(t *testing.T) {
	for _, tc := range []struct {
		name   string
		sink   command.Sink
		stderr func(*bytes.Buffer) io.Writer
		status int
		said   string
	}{
		{"a failed write", command.Unwritable{Err: errors.New("disk gone")}, plain, 0, "command event not recorded: disk gone"},
		{"a panicking write", panickingSink{}, plain, 0, "command event not recorded: writing the command event: panic: the sink broke"},
		{"a failed write, stderr refusing", command.Unwritable{Err: errors.New("disk gone")}, deaf, cli.Unrecorded, ""},
		{"housekeeping, stderr refusing", housekeepingSink{}, deaf, 0, ""},
	} {
		twin := NewTwin()
		twin.Process.Commands = tc.sink
		twin.Process.Err = tc.stderr(twin.Err)
		if status := Run(twin.Process, []string{"--help"}); status != tc.status || !strings.Contains(twin.Err.String(), tc.said) {
			t.Errorf("%s: status %d, stderr %q; want %d saying %q", tc.name, status, twin.Err, tc.status, tc.said)
		}
	}
}

func plain(b *bytes.Buffer) io.Writer { return b }

func deaf(*bytes.Buffer) io.Writer { return refusing{errors.New("stderr gone")} }

type panickingSink struct{}

func (panickingSink) Write(command.Event) error { panic("the sink broke") }

// housekeepingSink is a log that kept the event and could not prune.
type housekeepingSink struct{}

func (housekeepingSink) Write(command.Event) error { return command.Housekeeping{} }

// The duration covers reading the place: an identity read that takes a
// second shows in it.
func TestTheDurationCoversReadingTheIdentity(t *testing.T) {
	twin := NewTwin()
	clock := twin.Process.Clock.(*command.FixedClock)
	identity := twin.Process.IdentityFiles
	twin.Process.IdentityFiles = func(c git.Checkout) (files.Files, files.Files) {
		clock.At = clock.At.Add(time.Second)
		return identity(c)
	}
	if _, e := run(t, twin, "--help"); e.Duration < time.Second {
		t.Errorf("a run whose identity read took a second lasted %v", e.Duration)
	}
}

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
		for _, r := range cli.Lit2().Rows() {
			if !strings.Contains(twin.Out.String(), r.Name.String()) {
				t.Errorf("lit2 %q does not name %s on stdout", args, r.Name)
			}
		}
		if status != 0 || twin.Err.Len() != 0 || e.Command.Name != commandname.Help() {
			t.Errorf("lit2 %q: status %d, stderr %q, command %q; want 0, nothing, help", args, status, twin.Err, e.Command.Name)
		}
	}
}

func TestAnUnknownVerbIsUsage(t *testing.T) {
	twin := NewTwin()
	status, e := run(t, twin, "frobnicate")
	if status != 64 || !strings.HasPrefix(twin.Err.String(), "error (reason=unknown_verb): ") || reasonOf(e) != "unknown_verb" || e.Command.Name != commandname.Unparsed() {
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
	if e.Command.Name != commandname.Sync() {
		t.Errorf("lit2 admin sync recorded %#v", e.Command)
	}
}

// A row moved to another group is asked, logged and named by its body
// under its new name, with no edit beside the row's.
func TestARegroupedRowIsNamedByItsNewPlace(t *testing.T) {
	c := cli.Lit2()
	moved := commandname.InGroup("config", "sync")
	rows := c.Rows()
	for i, r := range rows {
		if r.Name == commandname.Sync() {
			rows[i].Name = moved
		}
	}
	regrouped, err := cli.NewCommands(rows, c.Bare())
	if err != nil {
		t.Fatal(err)
	}
	twin := NewTwin()
	e := invoke(twin.Process, asking(func() cli.Commands { return regrouped }, "config", "sync")).Event
	if e.Command.Name != moved || !errors.Is(failure(t, e), errNotBuilt) || !strings.Contains(failure(t, e).Error(), "lit2 config sync is not built") {
		t.Errorf("config sync recorded %#v ending %#v; want it asked as config sync, its failure naming config sync", e.Command, e.Outcome)
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
	if e.Place != command.Placed(want) {
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
