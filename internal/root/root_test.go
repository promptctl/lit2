package root

import (
	"bytes"
	"errors"
	"fmt"
	"os"
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
	"github.com/promptctl/lit2/internal/verbs"
)

// asking is what Run asks of lit2's command line built by commands.
func asking(commands func() cli.Commands, args ...string) func() (cli.Summary, func(body) cli.Outcome) {
	return func() (cli.Summary, func(body) cli.Outcome) { return parsed(commands, args) }
}

// recordedOf is the end of a run that made its event.
func recordedOf(t *testing.T, e command.Ended) command.Recorded {
	t.Helper()
	r, ok := e.(command.Recorded)
	if !ok {
		t.Fatalf("the run ended %#v; want it recorded", e)
	}
	return r
}

// A panic in any stage between the run's start and its event is recorded
// as lit2's failure, its value and stack kept, with what was known before
// it: what was asked, once asking ended, and how far the run got in
// reading its place.
func TestAPanicInAnyStageIsRecorded(t *testing.T) {
	found := command.AtCheckout{Checkout: twinCheckout}
	for _, tc := range []struct {
		stage string
		twin  func(Twin) Twin
		ask   func() (cli.Summary, func(body) cli.Outcome)
		asked commandname.Name
		where func(command.Where) bool
	}{
		{"building the command table", func(t Twin) Twin { return t }, asking(func() cli.Commands { panic("the table broke") }, "get", "x"), commandname.Unparsed(), func(w command.Where) bool { _, read := w.(command.Place); return read }},
		{"locating the checkout", func(t Twin) Twin {
			t.Process.Locate = func() command.Located { panic("the walk broke") }
			return t
		}, asking(cli.Lit2, "get", "x"), commandname.Get(), is(command.PlaceUnread{})},
		{"reading its identity", func(t Twin) Twin {
			t.Process.IdentityFiles = func(git.Checkout) (files.Files, files.Files) { panic("the identity broke") }
			return t
		}, asking(cli.Lit2, "get", "x"), commandname.Get(), is(found)},
		{"running the command", func(t Twin) Twin {
			t.Process.Out = panicking{}
			return t
		}, asking(cli.Lit2, "--help"), commandname.Help(), func(w command.Where) bool { _, read := w.(command.Place); return read }},
	} {
		ended := recordedOf(t, invoke(tc.twin(NewTwin()).Process, tc.ask))
		e := ended.Event
		var in command.Internal
		if !errors.As(failure(t, e), &in) || !strings.Contains(in.Error(), "broke") || len(in.Stack()) == 0 || ended.Status != 70 {
			t.Errorf("%s: ended %#v; want Internal with its value and stack, exit 70", tc.stage, e.Outcome())
		}
		if e.Command.Name != tc.asked || !tc.where(e.Place) {
			t.Errorf("%s: recorded %s at %#v; want %s", tc.stage, e.Command.Name, e.Place, tc.asked)
		}
	}
}

// A command that panics still ends with its end steps: the temps beside
// its checkout's files are scanned and said.
func TestACommandThatPanicsStillScansItsTemps(t *testing.T) {
	for name, tc := range map[string]struct {
		twin func(Twin) Twin
		ask  func() (cli.Summary, func(body) cli.Outcome)
	}{
		"its answer": {func(t Twin) Twin { t.Process.Out = panicking{}; return t }, asking(cli.Lit2, "--help")},
		"its parse":  {func(t Twin) Twin { return t }, asking(func() cli.Commands { panic("the table broke") }, "--help")},
	} {
		e := recordedOf(t, invoke(tc.twin(NewTwin()).Process, tc.ask)).Event
		if _, failed := e.Outcome().(cli.Failed); !failed || len(e.Store.Temps) == 0 {
			t.Errorf("a run whose %s panicked ended %#v with temps %#v; want it failed, every directory scanned", name, e.Outcome(), e.Store.Temps)
		}
		for _, l := range e.Store.Temps {
			if _, scanned := l.(command.TempsScanned); !scanned {
				t.Errorf("a run whose %s panicked left %#v; want each directory scanned", name, l)
			}
		}
	}
}

// is is whether a Where is want.
func is(want command.Where) func(command.Where) bool {
	return func(w command.Where) bool { return w == want }
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

// A run whose event cannot be made, its start or its end panicking, says
// the outcome's line when it has one, then that it went unrecorded, with
// the panic's stack, and ends unrecorded.
func TestARunWithNoEventIsSaidUnrecorded(t *testing.T) {
	for _, tc := range []struct {
		name  string
		broke func(Twin) Twin
		line  string
	}{
		{"the trace id", func(t Twin) Twin {
			t.Process.Entropy = refusingReader{}
			return t
		}, ""},
		{"the clock at the start", func(t Twin) Twin {
			t.Process.Clock = &brokenClock{after: 0}
			return t
		}, ""},
		{"the clock at the end", func(t Twin) Twin {
			t.Process.Clock = &brokenClock{after: 1}
			return t
		}, "error (reason=unknown_verb): "},
	} {
		twin := tc.broke(NewTwin())
		status := Run(twin.Process, []string{"frobnicate"})
		said := twin.Err.String()
		if status != cli.Unrecorded || len(twin.Commands.Events) != 0 || !strings.HasPrefix(said, tc.line) || !strings.Contains(said, "lit2: unrecorded: ") || !strings.Contains(said, "goroutine") {
			t.Errorf("%s failing: status %d, %d events, stderr %q; want %d, none, %q then the unrecorded line and its stack", tc.name, status, len(twin.Commands.Events), said, cli.Unrecorded, tc.line)
		}
	}
}

type refusingReader struct{}

func (refusingReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

// What a forge did stands when its run then goes unrecorded: its refusal
// is said beside the end's panic.
func TestAForgeThatGoesUnrecordedKeepsWhatItForged(t *testing.T) {
	twin := NewTwin()
	refused := twin.Forge(verbs.Set, verbs.Operand{Path: "no/such/path", Value: "x"})
	if refused == nil {
		t.Fatal("control: a forged set of no such path was not refused")
	}
	twin.Process.Clock = &brokenClock{after: 1}
	err := twin.Forge(verbs.Set, verbs.Operand{Path: "no/such/path", Value: "x"})
	if err == nil || !strings.Contains(err.Error(), "the clock broke") || !strings.Contains(err.Error(), refused.Error()) {
		t.Errorf("a refused forge whose end broke = %v; want the end's panic and %v", err, refused)
	}
}

// brokenClock is a clock that reads its first after times and then breaks.
type brokenClock struct{ after int }

func (c *brokenClock) Now() time.Time {
	if c.after == 0 {
		panic("the clock broke")
	}
	c.after--
	return time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
}

// A sink that fails or panics loses the event, said on stderr, with the
// panic's stack; when stderr refuses that too, the status says the run
// unrecorded.
// An event that panics to encode is lost to the log, said with its
// panic: the line never reached the file.
func TestAnEventThatPanicsToEncodeIsLost(t *testing.T) {
	dir := t.TempDir()
	e := recordedOf(t, invoke(NewTwin().Process, asking(cli.Lit2, "--help"))).Event
	e.Ending = command.Failed{Failure: unsayable{}, Line: command.Delivered{}}
	lost, ok := recorded(command.NewLog(command.Set{Value: dir}, noHome), e).(command.Lost)
	var in command.Internal
	if !ok || !errors.As(lost.Why, &in) || !strings.Contains(in.Error(), "no words for it") || len(in.Stack()) == 0 {
		t.Errorf("an event that panics to encode = %#v; want it lost for its panic", lost)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("the log holds %v, %v; want nothing written", entries, err)
	}
}

// noHome is a home that does not read, for a log whose XDG_STATE_HOME is
// used.
func noHome() (string, error) { return "", errors.New("this test has no home") }

// unsayable is a failure whose message panics.
type unsayable struct{}

func (unsayable) Error() string       { panic("no words for it") }
func (unsayable) Class() reason.Class { return reason.ClassRefused }
func (unsayable) Reason() string      { return "unsayable" }

func TestALostEventNeverLooksClean(t *testing.T) {
	for _, tc := range []struct {
		name   string
		sink   command.Sink
		status int
		said   []string
	}{
		{"a failed write", command.Unwritable{Err: errors.New("disk gone")}, 0, []string{"lit2: command event not recorded: disk gone\n"}},
		{"a panicking write", panickingSink{}, 0, []string{"lit2: command event not recorded: panic: the sink broke\n", "goroutine"}},
	} {
		twin := NewTwin()
		twin.Process.Commands = tc.sink
		status := Run(twin.Process, []string{"--help"})
		for _, s := range tc.said {
			if status != tc.status || !strings.Contains(twin.Err.String(), s) {
				t.Errorf("%s: status %d, stderr %q; want %d saying %q", tc.name, status, twin.Err, tc.status, s)
			}
		}
		deaf := NewTwin()
		deaf.Process.Commands = tc.sink
		deaf.Process.Err = refusing{errors.New("stderr gone")}
		if status := Run(deaf.Process, []string{"--help"}); status != cli.Unrecorded {
			t.Errorf("%s, stderr refusing: status %d; want %d, the one channel left", tc.name, status, cli.Unrecorded)
		}
	}
}

type panickingSink struct{}

func (panickingSink) Write(command.Event) command.Written { panic("the sink broke") }

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
	if got := cli.ExitCode(e.Outcome()); got != status {
		t.Errorf("lit2 %q exited %d but its event records %d", args, status, got)
	}
	return status, e
}

// failure is the failure e records, failing t when e records none.
func failure(t *testing.T, e command.Event) reason.Failure {
	t.Helper()
	failed, ok := e.Outcome().(cli.Failed)
	if !ok {
		t.Fatalf("the run ended %#v; want it failed", e.Outcome())
	}
	return failed.Failure
}

func reasonOf(e command.Event) string {
	failed, ok := e.Outcome().(cli.Failed)
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
	e := recordedOf(t, invoke(twin.Process, asking(func() cli.Commands { return regrouped }, "config", "sync"))).Event
	if e.Command.Name != moved || !errors.Is(failure(t, e), errNotBuilt) || !strings.Contains(failure(t, e).Error(), "lit2 config sync is not built") {
		t.Errorf("config sync recorded %#v ending %#v; want it asked as config sync, its failure naming config sync", e.Command, e.Outcome())
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
	if status, e := run(t, twin, "--help"); status != 0 || e.Outcome() != (cli.Done{}) || twin.Err.Len() != 0 {
		t.Errorf("--help into a pipe whose reader left: status %d, outcome %#v, stderr %q", status, e.Outcome(), twin.Err)
	}
}

// peek is a command log that keeps what stderr held when the event came.
type peek struct {
	stderr *bytes.Buffer
	saw    string
}

func (p *peek) Write(command.Event) command.Written {
	p.saw = p.stderr.String()
	return command.Kept{}
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

// A stderr that refuses the outcome's line is a fact of the event, beside
// the same run whose stderr took it; the status is the outcome's either way.
func TestAnErrorLineStderrRefusedIsRecorded(t *testing.T) {
	took := NewTwin()
	tookStatus, delivered := run(t, took, "frobnicate")
	refused := NewTwin()
	refused.Process.Err = refusing{fmt.Errorf("write /dev/stderr: %w", syscall.EPIPE)}
	refusedStatus, undelivered := run(t, refused, "frobnicate")
	tookLine, _ := delivered.Ending.(command.Failed)
	refusedLine, _ := undelivered.Ending.(command.Failed)
	u, ok := refusedLine.Line.(command.Undelivered)
	if tookLine.Line != (command.Delivered{}) || !ok || !errors.Is(u.Why, syscall.EPIPE) || tookStatus != refusedStatus || tookStatus == 0 {
		t.Errorf("stderr taking the line records %#v, status %d; refusing it records %#v, status %d; want delivered and undelivered with EPIPE, one failing status", delivered.Ending, tookStatus, undelivered.Ending, refusedStatus)
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
	if e.Place != command.Where(want) {
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
