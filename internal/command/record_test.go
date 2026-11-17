package command

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

var (
	t0       = time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
	checkout = git.Checkout{WorkTree: "/w", GitDir: "/w/.git", CommonDir: "/w/.git"}
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
		{"get", cli.Failed{Failure: Internal{Err: errors.New("not built")}}, 70},
	} {
		e := Record(&FixedClock{At: t0}, inv, func(_ time.Time, asked func(cli.Summary), _ *StoreFacts, _ func(VerbsFacts)) cli.Outcome {
			asked(cli.Summary{Name: tc.name})
			return tc.outcome
		})
		if e.Invocation != inv || e.Command.Name != tc.name || cli.ExitCode(e.Outcome) != tc.exit {
			t.Errorf("%s: the event is %#v; want the invocation, the summary and exit %d", tc.name, e, tc.exit)
		}
	}
}

func TestAPanickingBodyKeepsWhatWasAsked(t *testing.T) {
	e := Record(&FixedClock{At: t0}, inv, func(_ time.Time, asked func(cli.Summary), _ *StoreFacts, _ func(VerbsFacts)) cli.Outcome {
		cmd, err := cli.Parse(cli.Verbs(), cli.Admin(), []string{"get", "x"})
		if err != nil {
			t.Fatal(err)
		}
		asked(cli.Summarize(cmd))
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
	if !ok || !strings.Contains(in.Error(), "the body broke") || len(in.Stack) == 0 || cli.ExitCode(e.Outcome) != 70 {
		t.Errorf("a panic is %#v; want Internal with its value and stack, exit 70", failed.Failure)
	}
}

func TestWorkThatPanicsBeforeAskingIsUnparsed(t *testing.T) {
	e := Record(&FixedClock{At: t0}, inv, func(time.Time, func(cli.Summary), *StoreFacts, func(VerbsFacts)) cli.Outcome {
		panic("the parse broke")
	})
	if e.Command.Name != cli.Unparsed || cli.ExitCode(e.Outcome) != 70 {
		t.Errorf("a panic before asked recorded %#v, exit %d; want unparsed, exit 70", e.Command, cli.ExitCode(e.Outcome))
	}
}

func TestRecordHandsNowToWorkAndTimesIt(t *testing.T) {
	clock := &FixedClock{At: t0}
	var handed time.Time
	e := Record(clock, inv, func(now time.Time, _ func(cli.Summary), _ *StoreFacts, _ func(VerbsFacts)) cli.Outcome {
		handed = now
		clock.At = clock.At.Add(1500 * time.Millisecond)
		return cli.Done{}
	})
	if !handed.Equal(t0) || !e.Started.Equal(t0) || e.Duration != 1500*time.Millisecond {
		t.Errorf("work saw %v, Started %v, Duration %v; want %v, %v, 1.5s", handed, e.Started, e.Duration, t0, t0)
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
	m := decode(t, Event{Invocation: inv, Command: cli.Summary{Name: "help"}, Started: t0, Outcome: cli.Done{}})
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
		appends []store.Appended
		want    bool
	}{{nil, false}, {[]store.Appended{{Records: 1, Log: store.Created}}, true}} {
		m := decode(t, Event{Invocation: inv, Command: cli.Summary{Name: "set"}, Started: t0, Store: StoreFacts{Appends: tc.appends}, Verbs: &VerbsFacts{}, Outcome: cli.Done{}})
		v, _ := m["verbs"].(map[string]any)
		if v["written"] != tc.want {
			t.Errorf("with %d appends the verbs block is %v; want written %v", len(tc.appends), v, tc.want)
		}
	}
}

func TestAFailedLineSaysWhy(t *testing.T) {
	m := decode(t, Event{Invocation: inv, Command: cli.Summary{Name: cli.Unparsed}, Started: t0, Outcome: cli.Failed{Failure: Internal{Err: errors.New("broke")}}})
	if m["outcome"] != "internal" || m["reason"] != "internal" || m["error"] != "broke" || m["exit_code"] != 70.0 {
		t.Errorf("the line is %v; want internal, internal, broke, 70", m)
	}
}

func TestTheLineSaysWhereAndWho(t *testing.T) {
	m := decode(t, Event{Invocation: Invocation{
		Place: Unlocatable{Dir: "/gone", Err: errors.New("permission denied")},
		Agent: events.AgentFrom("claude"),
	}, Command: cli.Summary{Name: "help"}, Started: t0, Outcome: cli.Done{}})
	place, _ := m["place"].(map[string]any)
	unlocatable, _ := place["unlocatable"].(map[string]any)
	if unlocatable["dir"] != "/gone" || unlocatable["error"] != "permission denied" || m["agent"] != "claude" {
		t.Errorf("the line is %v; want the unlocatable dir and its error, agent claude", m)
	}
}
