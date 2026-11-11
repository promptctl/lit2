package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/address"
	"github.com/promptctl/lit2/internal/reason"
)

func parse(args ...string) (Command, error) { return Parse(Verbs(), Admin(), args) }

func TestEachRowParsesToItsCommand(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want Command
	}{
		{[]string{"get", "x", "y/title"}, Get{paths: []PathText{"x", "y/title"}}},
		{[]string{"rm", "x/labels/bug"}, Rm{paths: []PathText{"x/labels/bug"}}},
		{[]string{"set", "x/status", "done", "y/title", "two words"}, Set{pairs: []Pair{{"x/status", "done"}, {"y/title", "two words"}}}},
		{[]string{"add", "x/labels", "ready"}, Add{pairs: []Pair{{"x/labels", "ready"}}}},
		{[]string{"admin", "init"}, AdminInit{}},
		{[]string{"admin", "sync"}, AdminSync{}},
	} {
		got, err := parse(tc.args...)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Parse(%q) = %#v, %v; want %#v", tc.args, got, err, tc.want)
		}
	}
}

func TestHelpIsAskedForAnywhereOrByNothing(t *testing.T) {
	for _, args := range [][]string{{}, {"--help"}, {"get", "x", "--help"}, {"--help", "frobnicate"}, {"admin", "--help"}, {"--"}} {
		if got, err := parse(args...); err != nil || got != (Help{}) {
			t.Errorf("Parse(%q) = %#v, %v; want Help", args, got, err)
		}
	}
}

func TestDoubleDashEndsFlagsAndDashIsAValue(t *testing.T) {
	got, err := parse("get", "--", "--help")
	if err != nil || !reflect.DeepEqual(got, Get{paths: []PathText{"--help"}}) {
		t.Errorf("get -- --help = %#v, %v; want a get of the path --help", got, err)
	}
	got, err = parse("set", "x/body", "-")
	if err != nil || !reflect.DeepEqual(got, Set{pairs: []Pair{{"x/body", "-"}}}) {
		t.Errorf("set x/body - = %#v, %v; want - as the value", got, err)
	}
}

func TestNotALit2CommandLine(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want ParseCode
	}{
		{[]string{"frobnicate"}, UnknownVerb},
		{[]string{"admin", "frobnicate"}, UnknownVerb},
		{[]string{"get"}, Usage},
		{[]string{"set", "x/status"}, Usage},
		{[]string{"add", "x/labels", "a", "x/labels"}, Usage},
		{[]string{"admin"}, Usage},
		{[]string{"admin", "init", "extra"}, Usage},
		{[]string{"get", "x", "--frob"}, Usage},
	} {
		_, err := parse(tc.args...)
		var f ParseFailure
		if !errors.As(err, &f) || f.Code != tc.want || f.Class() != reason.ClassUsage {
			t.Errorf("Parse(%q) failed with %#v; want a usage-class failure with code %d", tc.args, err, tc.want)
		}
	}
}

func TestEveryParseCodeHasItsOwnWireName(t *testing.T) {
	seen := map[string]ParseCode{}
	for _, code := range []ParseCode{UnknownVerb, Usage} {
		name := ParseFailure{Code: code}.Reason()
		if prior, ok := seen[name]; ok || name == "" {
			t.Errorf("codes %d and %d share the wire name %q", prior, code, name)
		}
		seen[name] = code
	}
}

func TestHelpNamesEveryRow(t *testing.T) {
	help := HelpText(Verbs(), Admin(), address.FieldTable{})
	for prefix, table := range map[string]Table{"": Verbs(), "admin ": Admin()} {
		for _, r := range table.rows {
			if !strings.Contains(help, prefix+r.Name) || !strings.Contains(help, r.Summary) {
				t.Errorf("--help does not name %q with its summary %q", prefix+r.Name, r.Summary)
			}
		}
	}
}

func TestHelpNamesEveryField(t *testing.T) {
	fields, err := address.NewFieldTable(address.FieldRow{Name: "title", Help: "one line naming the work"})
	if err != nil {
		t.Fatal(err)
	}
	help := HelpText(Verbs(), Admin(), fields)
	if !strings.Contains(help, "title") || !strings.Contains(help, "one line naming the work") {
		t.Errorf("--help does not name the field title:\n%s", help)
	}
}

// The table is the parser: one more row parses and is helped with no other
// edit `[LAW:composability]`.
func TestOneMoreRowIsParsedAndHelped(t *testing.T) {
	show := Row{Name: "show", Operands: TakesPaths{Make: func(p []PathText) Command { return Get{paths: p} }}, Summary: "read, by another name"}
	verbs := Table{rows: append(Verbs().rows, show)}
	got, err := Parse(verbs, Admin(), []string{"show", "x"})
	if err != nil || !reflect.DeepEqual(got, Get{paths: []PathText{"x"}}) {
		t.Errorf("show x = %#v, %v; want the new row's command", got, err)
	}
	if help := HelpText(verbs, Admin(), address.FieldTable{}); !strings.Contains(help, "show") || !strings.Contains(help, show.Summary) {
		t.Errorf("--help does not name the new row:\n%s", help)
	}
}

// classed is a failure of one class, standing in for any layer's.
type classed reason.Class

func (c classed) Error() string       { return "a failure" }
func (c classed) Class() reason.Class { return reason.Class(c) }
func (c classed) Reason() string      { return "classed" }

func TestExitCodeIsTheClassContract(t *testing.T) {
	if got := ExitCode(Done{}); got != 0 {
		t.Errorf("Done exits %d; want 0", got)
	}
	for class, want := range map[reason.Class]int{
		reason.ClassRefused:     1,
		reason.ClassUsage:       64,
		reason.ClassInternal:    70,
		reason.ClassEnvironment: 78,
	} {
		if got := ExitCode(Failed{Failure: classed(class)}); got != want {
			t.Errorf("a %s failure exits %d; want %d", class, got, want)
		}
	}
}

func TestReportSaysTheReasonAndTheError(t *testing.T) {
	if got := Report(Done{}); got != "" {
		t.Errorf("Done reports %q; want nothing", got)
	}
	_, err := parse("frobnicate")
	var f ParseFailure
	if !errors.As(err, &f) {
		t.Fatalf("frobnicate parsed: %v", err)
	}
	if got := Report(Failed{Failure: f}); !strings.HasPrefix(got, "error (reason=unknown_verb): ") || !strings.Contains(got, "frobnicate") {
		t.Errorf("the error line is %q", got)
	}
}

func TestSummaryCountsValuesNeverHoldsThem(t *testing.T) {
	cmd, err := parse("set", "x/status", "done", "y/title", "secret words")
	if err != nil {
		t.Fatal(err)
	}
	got := Summarize(cmd)
	if got.Name != "set" || !reflect.DeepEqual(got.Paths, []PathText{"x/status", "y/title"}) || got.Values != 2 {
		t.Errorf("Summarize(set) = %#v; want set, both paths, two values", got)
	}
}
