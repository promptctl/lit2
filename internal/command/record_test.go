package command

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/git"
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
		Trace:   TraceID{1, 2, 3},
		Version: "test",
		Place:   InCheckout{Checkout: checkout, Identity: Minted{Checkout: store.CheckoutID{9}, Key: key}},
		Agent:   events.Unlabeled{},
	}
)

func TestRecordYieldsTheEventOfDoneAndOfFailed(t *testing.T) {
	for _, tc := range []struct {
		name    cli.CommandName
		outcome cli.Outcome
		exit    int
	}{
		{"help", cli.Done{}, 0},
		{"get", cli.Failed{Failure: Broke(errors.New("not built"))}, 70},
	} {
		given := asked(inv, tc.name)
		e := Record(&FixedClock{At: t0}, noGit, given, func(Recorder) cli.Outcome {
			return tc.outcome
		})
		if !reflect.DeepEqual(e.Invocation, given) || cli.ExitCode(e.Outcome) != tc.exit {
			t.Errorf("%s: the event is %#v; want the invocation, the summary and exit %d", tc.name, e, tc.exit)
		}
	}
}

func TestAPanickingBodyKeepsWhatWasAsked(t *testing.T) {
	cmd, err := cli.Parse(cli.Verbs(), cli.Groups(), []string{"get", "x"})
	if err != nil {
		t.Fatal(err)
	}
	e := Record(&FixedClock{At: t0}, noGit, asking(inv, cli.Summarize(cmd)), func(Recorder) cli.Outcome {
		panic("the body broke")
	})
	if e.Command.Name != "get" || len(e.Command.Paths) != 1 || e.Command.Paths[0] != "x" {
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

// The run started before Record did, when the place was read, so the
// duration runs from the reading Record is given.
func TestRecordTimesTheRunFromTheReadingItIsGiven(t *testing.T) {
	clock := &FixedClock{At: t0.Add(250 * time.Millisecond)}
	e := Record(clock, noGit, asked(inv, "help"), func(Recorder) cli.Outcome {
		clock.At = clock.At.Add(1500 * time.Millisecond)
		return cli.Done{}
	})
	if !e.Started.Equal(t0) || e.Duration != 1750*time.Millisecond {
		t.Errorf("Started %v, Duration %v; want %v, 1.75s", e.Started, e.Duration, t0)
	}
}

// The version in force is known only once the fold has read it, so the
// body gives it, and a run that read none has no config block at all.
func TestTheVersionInForceIsRecordedOnceTheBodyGivesIt(t *testing.T) {
	given := ConfigFacts{Ref: "ab12", Facts: config.Facts{Gate: "none"}}
	e := Record(&FixedClock{At: t0}, noGit, asked(inv, "get"), func(r Recorder) cli.Outcome {
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
	none := Record(&FixedClock{At: t0}, noGit, asked(inv, "get"), func(Recorder) cli.Outcome { return cli.Done{} })
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
	m := decode(t, Event{Invocation: asked(inv, "help"), Outcome: cli.Done{}})
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
		m := decode(t, Event{Invocation: asked(inv, "set"), Store: StoreFacts{Appends: tc.appends}, Verbs: &VerbsFacts{}, Outcome: cli.Done{}})
		v, _ := m["verbs"].(map[string]any)
		if v["written"] != tc.want {
			t.Errorf("with %d appends the verbs block is %v; want written %v", len(tc.appends), v, tc.want)
		}
	}
}

func TestAFailedLineSaysWhy(t *testing.T) {
	m := decode(t, Event{Invocation: asked(inv, cli.Unparsed), Outcome: cli.Failed{Failure: Broke(errors.New("broke"))}})
	if m["outcome"] != "internal" || m["reason"] != "internal" || m["error"] != "broke" || m["exit_code"] != 70.0 {
		t.Errorf("the line is %v; want internal, internal, broke, 70", m)
	}
}

func TestTheLineSaysWhereAndWho(t *testing.T) {
	m := decode(t, Event{Invocation: Invocation{
		Place:   Unlocatable{Dir: "/gone", Err: errors.New("permission denied")},
		Agent:   events.AgentFrom("claude"),
		Command: cli.Summary{Name: "help"},
		Started: t0,
	}, Outcome: cli.Done{}})
	place, _ := m["place"].(map[string]any)
	unlocatable, _ := place["unlocatable"].(map[string]any)
	if unlocatable["dir"] != "/gone" || unlocatable["error"] != "permission denied" || m["agent"] != "claude" {
		t.Errorf("the line is %v; want the unlocatable dir and its error, agent claude", m)
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
	e := Record(&FixedClock{At: t0}, func() []git.Spawn { return ran }, asked(inv, "get"), func(Recorder) cli.Outcome { return cli.Done{} })
	block, _ := decode(t, e)["git"].([]any)
	if len(block) != 2 {
		t.Fatalf("the git block is %v; want both spawns", block)
	}
	exited, _ := block[0].(map[string]any)
	unstarted, _ := block[1].(map[string]any)
	if exited["exit"] != 128.0 || exited["stderr"] != "fatal: not a git repository" || unstarted["unstarted"] != "git is not on PATH" || unstarted["exit"] != nil {
		t.Errorf("the git block is %v; want git's exit and stderr, and why the other never started", block)
	}
	none := Record(&FixedClock{At: t0}, noGit, asked(inv, "get"), func(Recorder) cli.Outcome { return cli.Done{} })
	if block, ok := decode(t, none)["git"].([]any); !ok || len(block) != 0 {
		t.Errorf("a run with no git logged %v; want an empty block", decode(t, none)["git"])
	}
}

// joined is a failure that holds an Internal beside another, as a run
// that broke after a write that was not durable does.
type joined struct{ Internal }

func (j joined) Unwrap() []error { return []error{j.Internal, errors.New("and a write not durable")} }

func TestAnInternalJoinedWithAnotherFailureKeepsItsStack(t *testing.T) {
	broke := Record(&FixedClock{At: t0}, noGit, asked(inv, "get"), func(Recorder) cli.Outcome { panic("the body broke") }).Outcome.(cli.Failed).Failure.(Internal)
	m := decode(t, Event{Invocation: asked(inv, "get"), Outcome: cli.Failed{Failure: joined{broke}}})
	if stack, _ := m["stack"].(string); stack == "" || stack != string(broke.Stack()) {
		t.Errorf("the line's stack is %q; want the Internal's", m["stack"])
	}
}

// asked is inv asking for the command name, started at t0.
func asked(inv Invocation, name cli.CommandName) Invocation {
	return asking(inv, cli.Summary{Name: name})
}

// asking is inv asking for s, started at t0.
func asking(inv Invocation, s cli.Summary) Invocation {
	inv.Command, inv.Started = s, t0
	return inv
}
