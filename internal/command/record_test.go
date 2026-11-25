package command

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/commandname"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/reason"
	"github.com/promptctl/lit2/internal/store"
)

var (
	t0       = time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
	checkout = git.Checkout{WorkTree: "/w", GitDir: "/w/.git", CommonDir: "/w/.git", Format: git.SHA256}
	key      = func() events.Key {
		k, err := events.NewKey(strings.NewReader(strings.Repeat("k", 32)))
		if err != nil {
			panic(err)
		}
		return k
	}()
	inv = Invocation{
		Trace:    TraceID{1, 2, 3},
		Version:  "test",
		LitAgent: Unset{},
		Started:  t0,
	}
	here = InCheckout{Checkout: checkout, Identity: Minted{Checkout: store.CheckoutID{9}, Key: key}}
)

// beginning is a start that reads as inv.
func beginning(inv Invocation) func() Invocation { return func() Invocation { return inv } }

// summary is the command line that asked for name.
func summary(name commandname.Name) cli.Summary { return cli.Summary{Name: name} }

// stages is a run begun as inv, asked s, its checkout found and placed at
// at, answering answer and ending with the answer's outcome, its line
// taken by a say that keeps nothing.
func stages(s cli.Summary, at Place, answer func(Run) cli.Outcome) Stages[struct{}] {
	return Stages[struct{}]{
		Begin:    beginning(inv),
		Ask:      func() (cli.Summary, struct{}) { return s, struct{}{} },
		Locate:   func() Located { return AtCheckout{Checkout: checkout} },
		Identify: func(Located) Place { return at },
		Answer:   func(_ struct{}, r Run) cli.Outcome { return answer(r) },
		End:      func(_ Run, o cli.Outcome) cli.Outcome { return o },
		Say:      func(string) error { return nil },
	}
}

// recordedOf is the event of a run that made one.
func recordedOf(t *testing.T, e Ended) Recorded {
	t.Helper()
	r, ok := e.(Recorded)
	if !ok {
		t.Fatalf("the run ended %#v; want it recorded", e)
	}
	return r
}

// record is the event of answer, asked s, run at at.
func record(t *testing.T, c Clock, spawns func() []git.Spawn, s cli.Summary, at Place, answer func(Run) cli.Outcome) Event {
	t.Helper()
	return recordedOf(t, Record(c, spawns, stages(s, at, answer))).Event
}

// The place recorded is how far the run got and, once read, where it is
// held: where the answer moved it, a panic after the move included.
func TestTheEventKeepsHowFarTheRunGotInReadingItsPlace(t *testing.T) {
	unminted := InCheckout{Checkout: checkout, Identity: Unminted{Missing: Both{}}}
	for _, tc := range []struct {
		name string
		then func()
	}{
		{"an answer that ends", func() {}},
		{"an answer that panics after the move", func() { panic("the body broke") }},
	} {
		e := record(t, &FixedClock{At: t0}, noGit, summary(commandname.Init()), unminted, func(r Run) cli.Outcome {
			r.Move(here)
			tc.then()
			return cli.Done{}
		})
		if e.Place != Where(here) {
			t.Errorf("%s: recorded the place %#v; want the one it moved to, %#v", tc.name, e.Place, here)
		}
	}
	found := AtCheckout{Checkout: checkout}
	locating, identifying := stages(summary(commandname.Get()), here, nil), stages(summary(commandname.Get()), here, nil)
	locating.Locate = func() Located { panic("locating") }
	identifying.Identify = func(Located) Place { panic("reading the identity") }
	for _, tc := range []struct {
		name   string
		stages Stages[struct{}]
		where  Where
		line   string
	}{
		{"before git said where the run is", locating, PlaceUnread{}, `"place":{"unread":{}}`},
		{"between finding the checkout and its identity", identifying, found, `"identity":{"unread":{}}`},
	} {
		e := recordedOf(t, Record(&FixedClock{At: t0}, noGit, tc.stages)).Event
		line, err := encode(e)
		if e.Place != tc.where || err != nil || !strings.Contains(string(line), tc.line) {
			t.Errorf("a panic %s recorded %#v, said %s; want %#v, saying %s", tc.name, e.Place, line, tc.where, tc.line)
		}
	}
}

// What was asked is what Ask gave; a panic in Ask leaves it unparsed.
func TestTheEventKeepsWhatWasAsked(t *testing.T) {
	said := cli.Summary{Name: commandname.Get(), Paths: []cli.PathText{"x"}}
	after := stages(said, here, nil)
	after.Locate = func() Located { panic("after asking") }
	if e := recordedOf(t, Record(&FixedClock{At: t0}, noGit, after)).Event; !reflect.DeepEqual(e.Command, said) {
		t.Errorf("a panic after asking recorded %#v; want %#v", e.Command, said)
	}
	asking := stages(said, here, nil)
	asking.Ask = func() (cli.Summary, struct{}) { panic("asking") }
	if e := recordedOf(t, Record(&FixedClock{At: t0}, noGit, asking)).Event; !reflect.DeepEqual(e.Command, cli.Unparsed()) {
		t.Errorf("a panic while asking recorded %#v; want unparsed", e.Command)
	}
}

// End runs over an answer that panicked as over any other outcome, so a
// run that crashed still says what its End finds; a panic in End is the
// run's outcome.
func TestEndRunsOverAnAnswerThatPanicked(t *testing.T) {
	scanned := []Leftovers{TempsScanned{Dir: "/w/.git/lit/checkout"}}
	answering := stages(summary(commandname.Get()), here, func(Run) cli.Outcome { panic("the answer broke") })
	var given cli.Outcome
	answering.End = func(r Run, o cli.Outcome) cli.Outcome {
		given = o
		r.Stored().Temps = scanned
		return o
	}
	e := recordedOf(t, Record(&FixedClock{At: t0}, noGit, answering)).Event
	in, _ := given.(cli.Failed).Failure.(Internal)
	if !strings.Contains(in.Error(), "the answer broke") || len(in.Stack()) == 0 || !reflect.DeepEqual(e.Outcome, given) || !reflect.DeepEqual(e.Store.Temps, scanned) {
		t.Errorf("End was given %v and the event holds %v, temps %v; want End over the answer's panic, its temps kept", given, e.Outcome, e.Store.Temps)
	}
	ending := stages(summary(commandname.Get()), here, func(Run) cli.Outcome { return cli.Done{} })
	ending.End = func(Run, cli.Outcome) cli.Outcome { panic("the end broke") }
	if in, _ := recordedOf(t, Record(&FixedClock{At: t0}, noGit, ending)).Event.Outcome.(cli.Failed).Failure.(Internal); !strings.Contains(in.Error(), "the end broke") || len(in.Stack()) == 0 {
		t.Errorf("a panic in End ended %v; want its Internal", in)
	}
}

// A panic whose value is an error keeps it in the chain.
func TestAPanickedErrorStaysInTheChain(t *testing.T) {
	cause := errors.New("the cause")
	in, _ := record(t, &FixedClock{At: t0}, noGit, summary(commandname.Get()), here, func(Run) cli.Outcome { panic(cause) }).Outcome.(cli.Failed).Failure.(Internal)
	if !errors.Is(in, cause) {
		t.Errorf("a panic with an error ended %v; want the error in its chain", in)
	}
}

// The line and the status are the outcome's, said under the recover: a
// failure whose rendering panics ends as the Internal of that panic.
func TestALineThatPanicsToRenderEndsInternal(t *testing.T) {
	s := stages(summary(commandname.Get()), here, func(Run) cli.Outcome { return cli.Failed{Failure: unsayable{}} })
	var line string
	s.Say = func(l string) error { line += l; return nil }
	ended := recordedOf(t, Record(&FixedClock{At: t0}, noGit, s))
	in, ok := ended.Event.Outcome.(cli.Failed).Failure.(Internal)
	if !ok || !strings.Contains(in.Error(), "no words for it") || ended.Status != 70 || !strings.Contains(line, "no words for it") {
		t.Errorf("a failure that cannot be said ended %#v, saying %q; want an Internal of its panic, said, exit 70", ended, line)
	}
}

// What became of the error line is the event's: a line Say took, one it
// refused or panicked on, and an outcome with no line make events that
// differ in that alone.
func TestTheEventSaysWhetherItsErrorLineWasDelivered(t *testing.T) {
	refused := cli.Failed{Failure: Broke(errors.New("refused"))}
	run := func(o cli.Outcome, say func(string) error) Event {
		s := stages(summary(commandname.Get()), here, func(Run) cli.Outcome { return o })
		s.Say = say
		return recordedOf(t, Record(&FixedClock{At: t0}, noGit, s)).Event
	}
	stderrGone := errors.New("stderr is closed")
	var said []string
	taking := func(l string) error { said = append(said, l); return nil }
	took := run(refused, taking)
	refusedLine := run(refused, func(string) error { return stderrGone })
	panicked := run(refused, func(string) error { panic("stderr broke") })
	done := run(cli.Done{}, taking)
	if !slices.Equal(said, []string{cli.Report(refused)}) {
		t.Errorf("say was handed %q; want the refused run's line once and nothing for the done one", said)
	}
	if took.ErrorLine != (LineDelivered{}) || done.ErrorLine != (NoErrorLine{}) {
		t.Errorf("a taken line is %#v and no line is %#v; want delivered and none", took.ErrorLine, done.ErrorLine)
	}
	if u, ok := refusedLine.ErrorLine.(LineUndelivered); !ok || !errors.Is(u.Why, stderrGone) {
		t.Errorf("a refused line is %#v; want undelivered, and why", refusedLine.ErrorLine)
	}
	if u, ok := panicked.ErrorLine.(LineUndelivered); !ok || !strings.Contains(u.Why.Error(), "stderr broke") {
		t.Errorf("a say that panicked is %#v; want undelivered, with its panic", panicked.ErrorLine)
	}
	refusedLine.ErrorLine = took.ErrorLine
	if !reflect.DeepEqual(took, refusedLine) {
		t.Errorf("a taken and a refused line differ beyond their delivery:\n%#v\n%#v", took, refusedLine)
	}
	for _, tc := range []struct {
		e    Event
		want map[string]any
	}{
		{took, map[string]any{"error_line": "delivered"}},
		{run(refused, func(string) error { return stderrGone }), map[string]any{"error_line": "undelivered", "error_line_undelivered": "stderr is closed"}},
		{done, map[string]any{"error_line": "none"}},
	} {
		m := decode(t, tc.e)
		for k, v := range tc.want {
			if m[k] != v {
				t.Errorf("the line's %s is %v; want %v", k, m[k], v)
			}
		}
		if _, has := tc.want["error_line_undelivered"]; !has && m["error_line_undelivered"] != nil {
			t.Errorf("a line that was not refused says %v", m["error_line_undelivered"])
		}
	}
}

// A read the event is made of that panics leaves no event: at the start
// there is no outcome to say; at the end the outcome is still said, with
// the stack of a panic that ended it, which no event keeps.
func TestAReadTheEventIsMadeOfThatPanicsIsUnrecorded(t *testing.T) {
	refused := cli.Failed{Failure: Broke(errors.New("refused"))}
	refusing := stages(summary(commandname.Get()), here, func(Run) cli.Outcome { return refused })
	starting := refusing
	starting.Begin = func() Invocation { panic("the start broke") }
	panicked := stages(summary(commandname.Get()), here, func(Run) cli.Outcome { panic("the answer broke") })
	spawnsBroke := func() []git.Spawn { panic("the spawns broke") }
	for _, tc := range []struct {
		name   string
		clock  Clock
		spawns func() []git.Spawn
		stages Stages[struct{}]
		said   func(string) bool
	}{
		{"the start", &FixedClock{At: t0}, noGit, starting, func(s string) bool { return s == "" }},
		{"the clock at the end", panicking{}, noGit, refusing, func(s string) bool { return s == cli.Report(refused) }},
		{"the git spawns", &FixedClock{At: t0}, spawnsBroke, refusing, func(s string) bool { return s == cli.Report(refused) }},
		{"the git spawns after the answer panicked", &FixedClock{At: t0}, spawnsBroke, panicked, func(s string) bool {
			return strings.Contains(s, "the answer broke") && strings.Contains(s, "goroutine")
		}},
	} {
		// the Unrecorded says the line, so Say is never handed it
		var told []string
		tc.stages.Say = func(l string) error { told = append(told, l); return nil }
		e, ok := Record(tc.clock, tc.spawns, tc.stages).(Unrecorded)
		if !ok || !tc.said(e.Said) || !strings.Contains(e.Broke.Error(), "broke") || len(e.Broke.Stack()) == 0 || len(told) != 0 {
			t.Errorf("%s panicking ended %#v, Say handed %q; want unrecorded with its panic and stack, and what it said, said once", tc.name, e, told)
		}
	}
}

// panicking is a clock that breaks when read.
type panicking struct{}

func (panicking) Now() time.Time { panic("the clock broke") }

// unsayable is a failure whose message panics.
type unsayable struct{}

func (unsayable) Error() string       { panic("no words for it") }
func (unsayable) Class() reason.Class { return reason.ClassRefused }
func (unsayable) Reason() string      { return "unsayable" }

func TestRecordYieldsTheEventOfDoneAndOfFailed(t *testing.T) {
	for _, tc := range []struct {
		name    commandname.Name
		outcome cli.Outcome
		exit    int
	}{
		{commandname.Help(), cli.Done{}, 0},
		{commandname.Get(), cli.Failed{Failure: Broke(errors.New("not built"))}, 70},
	} {
		e := record(t, &FixedClock{At: t0}, noGit, summary(tc.name), here, func(Run) cli.Outcome {
			return tc.outcome
		})
		if !reflect.DeepEqual(e.Invocation, inv) || !reflect.DeepEqual(e.Command, summary(tc.name)) || cli.ExitCode(e.Outcome) != tc.exit {
			t.Errorf("%s: the event is %#v; want the invocation, the summary and exit %d", tc.name, e, tc.exit)
		}
	}
}

func TestAPanickingBodyKeepsWhatWasAsked(t *testing.T) {
	p, err := cli.Parse(cli.Lit2(), []string{"get", "x"})
	if err != nil {
		t.Fatal(err)
	}
	e := record(t, &FixedClock{At: t0}, noGit, p.Asked, here, func(Run) cli.Outcome {
		panic("the body broke")
	})
	if e.Command.Name != commandname.Get() || len(e.Command.Paths) != 1 || e.Command.Paths[0] != "x" {
		t.Errorf("a panic in get x recorded %#v; want get with its one path", e.Command)
	}
	failed, ok := e.Outcome.(cli.Failed)
	if !ok {
		t.Fatalf("a panic ended %#v; want Failed", e.Outcome)
	}
	in, ok := failed.Failure.(Internal)
	if !ok || !strings.Contains(in.Error(), "the body broke") || len(in.Stack()) == 0 || cli.ExitCode(e.Outcome) != 70 {
		t.Errorf("a panic is %#v; want Internal with its value and stack, exit 70", failed.Failure)
	}
}

// The run's now is Begin's reading, and its duration runs from it.
func TestRecordTimesTheRunFromBeginsReading(t *testing.T) {
	clock := &FixedClock{At: t0.Add(250 * time.Millisecond)}
	var now time.Time
	e := record(t, clock, noGit, summary(commandname.Help()), here, func(r Run) cli.Outcome {
		now = r.Started()
		clock.At = clock.At.Add(1500 * time.Millisecond)
		return cli.Done{}
	})
	if !e.Started.Equal(t0) || !now.Equal(t0) || e.Duration != 1750*time.Millisecond {
		t.Errorf("Started %v, the answer's now %v, Duration %v; want %v, %v, 1.75s", e.Started, now, e.Duration, t0, t0)
	}
}

// The version in force is known only once the fold has read it, so the
// body gives it, and a run that read none has no config block at all.
func TestTheVersionInForceIsRecordedOnceTheBodyGivesIt(t *testing.T) {
	given := ConfigFacts{Ref: "ab12", Facts: config.Facts{Gate: "none"}}
	e := record(t, &FixedClock{At: t0}, noGit, summary(commandname.Get()), here, func(r Run) cli.Outcome {
		r.Configured(given)
		panic("after the fold")
	})
	if e.Config == nil || *e.Config != given {
		t.Fatalf("the event's config is %+v; want %+v kept through the panic", e.Config, given)
	}
	block, _ := decode(t, e)["config"].(map[string]any)
	counts, _ := block["counts"].(map[string]any)
	if block["ref"] != "ab12" || counts["levels"] != 0.0 {
		t.Errorf("the config block is %v; want the ref and every zero count written", block)
	}
	none := record(t, &FixedClock{At: t0}, noGit, summary(commandname.Get()), here, func(Run) cli.Outcome { return cli.Done{} })
	if _, ok := decode(t, none)["config"]; ok || none.Config != nil {
		t.Error("a run that read no version has a config block")
	}
}
func decode(t *testing.T, e Event) map[string]any {
	t.Helper()
	b, err := encode(e)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("the line %s does not decode: %v", b, err)
	}
	return m
}

func TestTheLineCarriesTheDefaultFieldsAndZeroCounts(t *testing.T) {
	m := decode(t, Event{Invocation: inv, Command: summary(commandname.Help()), Place: here, Outcome: cli.Done{}})
	for _, field := range []string{"event", "trace_id", "service", "started_at", "duration_ms", "outcome", "exit_code", "version", "command", "counts", "place"} {
		if _, ok := m[field]; !ok {
			t.Errorf("the line has no %s: %v", field, m)
		}
	}
	counts, _ := m["counts"].(map[string]any)
	if counts["paths"] != 0.0 || counts["values"] != 0.0 {
		t.Errorf("counts are %v; want paths and values present as zero", m["counts"])
	}
	if m["event"] != "lit2 help" || m["outcome"] != "ok" || m["exit_code"] != 0.0 {
		t.Errorf("the line is %v; want lit2 help, ok, exit 0", m)
	}
	if _, ok := m["error"]; ok {
		t.Errorf("an ok line carries an error: %v", m)
	}
}

func TestAVerbsLineSaysWhetherTheStoreTookAnAppend(t *testing.T) {
	for _, tc := range []struct {
		appends []store.Appending
		want    bool
	}{
		{nil, false},
		{[]store.Appending{store.Appended{Written: store.Written{Records: 1}, Log: store.LogLookedUp{}}}, true},
		{[]store.Appending{store.AppendedNotDurable{Written: store.Written{Records: 1}, Log: store.LogLookedUp{}, Why: errors.New("fsync failed")}}, true},
		{[]store.Appending{store.NotAppended{Log: store.NoLog{}, Why: errors.New("no space")}}, false},
	} {
		m := decode(t, Event{Invocation: inv, Command: summary(commandname.Set()), Place: here, Store: StoreFacts{Appends: tc.appends}, Verbs: &VerbsFacts{}, Outcome: cli.Done{}})
		v, _ := m["verbs"].(map[string]any)
		if v["written"] != tc.want {
			t.Errorf("with %d appends the verbs block is %v; want written %v", len(tc.appends), v, tc.want)
		}
	}
}

func TestAFailedLineSaysWhy(t *testing.T) {
	m := decode(t, Event{Invocation: inv, Command: summary(commandname.Unparsed()), Place: here, Outcome: cli.Failed{Failure: Broke(errors.New("broke"))}})
	if m["outcome"] != "internal" || m["reason"] != "internal" || m["error"] != "broke" || m["exit_code"] != 70.0 {
		t.Errorf("the line is %v; want internal, internal, broke, 70", m)
	}
}

func TestTheLineSaysWhereAndWho(t *testing.T) {
	m := decode(t, Event{Invocation: Invocation{
		LitAgent: Set{Value: "claude"},
		Started:  t0,
	}, Command: summary(commandname.Help()), Place: Unlocatable{Dir: "/gone", Err: errors.New("permission denied")}, Outcome: cli.Done{}})
	place, _ := m["place"].(map[string]any)
	unlocatable, _ := place["unlocatable"].(map[string]any)
	if unlocatable["dir"] != "/gone" || unlocatable["error"] != "permission denied" || m["agent"] != "claude" {
		t.Errorf("the line is %v; want the unlocatable dir and its error, agent claude", m)
	}
}

// LIT_AGENT unset and set empty both leave a run unlabeled, and its line
// says which; a label is said as the agent, with no why.
func TestTheLineSaysWhyARunIsUnlabeled(t *testing.T) {
	for _, tc := range []struct {
		name      string
		v         Variable
		agent     events.Agent
		said, why any
	}{
		{"unset", Unset{}, events.Unlabeled{}, nil, "unset"},
		{"set empty", Set{Value: ""}, events.Unlabeled{}, nil, "empty"},
		{"set", Set{Value: "claude"}, events.AgentFrom("claude"), "claude", nil},
	} {
		in := inv
		in.LitAgent = tc.v
		m := decode(t, Event{Invocation: in, Command: summary(commandname.Help()), Place: here, Outcome: cli.Done{}})
		if AgentOf(tc.v) != tc.agent || m["agent"] != tc.said || m["agent_unlabeled"] != tc.why {
			t.Errorf("LIT_AGENT %s labels %#v, and the line says agent %v, unlabeled %v; want %#v, %v, %v", tc.name, AgentOf(tc.v), m["agent"], m["agent_unlabeled"], tc.agent, tc.said, tc.why)
		}
	}
}

func noGit() []git.Spawn { return []git.Spawn{} }

// Every git process the invocation ran is on its logged line, how each
// ended included, and a run that ran none logs an empty block.
func TestEverySpawnReachesTheLoggedLine(t *testing.T) {
	ran := []git.Spawn{
		{Args: []string{"rev-parse"}, Ended: git.Exited{Code: 128}, Stderr: "fatal: not a git repository"},
		{Args: []string{"hash-object"}, Ended: git.Unstarted{Why: errors.New("git is not on PATH")}},
	}
	e := record(t, &FixedClock{At: t0}, func() []git.Spawn { return ran }, summary(commandname.Get()), here, func(Run) cli.Outcome { return cli.Done{} })
	block, _ := decode(t, e)["git"].([]any)
	if len(block) != 2 {
		t.Fatalf("the git block is %v; want both spawns", block)
	}
	exited, _ := block[0].(map[string]any)
	unstarted, _ := block[1].(map[string]any)
	if exited["exit"] != 128.0 || exited["stderr"] != "fatal: not a git repository" || unstarted["unstarted"] != "git is not on PATH" || unstarted["exit"] != nil {
		t.Errorf("the git block is %v; want git's exit and stderr, and why the other never started", block)
	}
	none := record(t, &FixedClock{At: t0}, noGit, summary(commandname.Get()), here, func(Run) cli.Outcome { return cli.Done{} })
	if block, ok := decode(t, none)["git"].([]any); !ok || len(block) != 0 {
		t.Errorf("a run with no git logged %v; want an empty block", decode(t, none)["git"])
	}
}

// joined is a failure that holds an Internal beside another, as a run
// that broke after a write that was not durable does.
type joined struct{ Internal }

func (j joined) Unwrap() []error { return []error{j.Internal, errors.New("and a write not durable")} }

func TestAnInternalJoinedWithAnotherFailureKeepsItsStack(t *testing.T) {
	broke := record(t, &FixedClock{At: t0}, noGit, summary(commandname.Get()), here, func(Run) cli.Outcome { panic("the body broke") }).Outcome.(cli.Failed).Failure.(Internal)
	m := decode(t, Event{Invocation: inv, Command: summary(commandname.Get()), Place: here, Outcome: cli.Failed{Failure: joined{broke}}})
	if stack, _ := m["stack"].(string); stack == "" || stack != string(broke.Stack()) {
		t.Errorf("the line's stack is %q; want the Internal's", m["stack"])
	}
}
