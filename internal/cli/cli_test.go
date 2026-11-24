package cli

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/reason"
	"github.com/promptctl/lit2/internal/verbs"
)

// fields is the default profile's field table.
func fields(t *testing.T) config.Table {
	t.Helper()
	running, err := config.ReadReaderVersion("v0.4.0")
	if err != nil {
		t.Fatal(err)
	}
	schema, err := config.Load(assets.DefaultWorkspace.Bytes(), running)
	if err != nil {
		t.Fatal(err)
	}
	return schema.Table()
}

func parse(args ...string) (Command, error) { return Parse(Verbs(), Groups(), args) }

func TestEachRowParsesToItsCommand(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want Command
	}{
		{[]string{"get", "x", "y/title"}, Get{paths: []PathText{"x", "y/title"}}},
		{[]string{"rm", "x/labels/bug"}, Rm{paths: []PathText{"x/labels/bug"}}},
		{[]string{"set", "x/status", "done", "y/title", "two words"}, Set{pairs: []Pair{{"x/status", "done"}, {"y/title", "two words"}}}},
		{[]string{"add", "x/labels", "ready"}, Add{pairs: []Pair{{"x/labels", "ready"}}}},
		{[]string{"rm", "x", "--dry-run"}, Rm{paths: []PathText{"x"}, flags: Flags{DryRun: true}}},
		{[]string{"--dry-run", "add", "x/labels", "ready"}, Add{pairs: []Pair{{"x/labels", "ready"}}, flags: Flags{DryRun: true}}},
		{[]string{"admin", "init"}, AdminInit{}},
		{[]string{"admin", "sync"}, AdminSync{}},
		{[]string{"config", "commit"}, ConfigCommit{}},
		{[]string{"config", "status"}, ConfigStatus{}},
	} {
		got, err := parse(tc.args...)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Parse(%q) = %#v, %v; want %#v", tc.args, got, err, tc.want)
		}
	}
}

func TestHelpIsAskedForAnywhere(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"get", "x", "--help"}, {"--help", "frobnicate"}, {"admin", "--help"}, {"--bogus", "--help"}, {"get", "--bogus", "--help"}} {
		if got, err := parse(args...); err != nil || got != (Help{}) {
			t.Errorf("Parse(%q) = %#v, %v; want Help", args, got, err)
		}
	}
}

func TestABareLit2IsAGetOfTheRoot(t *testing.T) {
	for _, args := range [][]string{{}, {"--"}} {
		if got, err := parse(args...); err != nil || !reflect.DeepEqual(got, Get{paths: []PathText{"/"}}) {
			t.Errorf("Parse(%q) = %#v, %v; want get /", args, got, err)
		}
	}
	if _, err := parse("--dry-run"); !errors.Is(err, errNoDryRun) {
		t.Errorf("lit2 --dry-run = %v; want errNoDryRun: a get takes no --dry-run", err)
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
		args, control []string
		code          ParseCode
		want          error
	}{
		{[]string{"frobnicate"}, []string{"get", "x"}, UnknownVerb, errUnknownVerb},
		{[]string{"admin", "frobnicate"}, []string{"admin", "init"}, UnknownVerb, errUnknownVerb},
		{[]string{"get"}, []string{"get", "x"}, Usage, errNoPaths},
		{[]string{"set", "x/status"}, []string{"set", "x/status", "v"}, Usage, errNotPairs},
		{[]string{"add", "x/labels", "a", "x/labels"}, []string{"add", "x/labels", "a"}, Usage, errNotPairs},
		{[]string{"admin"}, []string{"admin", "init"}, Usage, errNoCommand},
		{[]string{"config"}, []string{"config", "commit"}, Usage, errNoCommand},
		{[]string{"config", "frobnicate"}, []string{"config", "commit"}, UnknownVerb, errUnknownVerb},
		{[]string{"config", "commit", "--dry-run"}, []string{"config", "commit"}, Usage, errNoDryRun},
		{[]string{"admin", "init", "extra"}, []string{"admin", "init"}, Usage, errOperands},
		{[]string{"get", "x", "--frob"}, []string{"get", "x"}, Usage, errUnknownFlag},
		{[]string{"get", "x", "--dry-run"}, []string{"get", "x"}, Usage, errNoDryRun},
		{[]string{"admin", "init", "--dry-run"}, []string{"admin", "init"}, Usage, errNoDryRun},
	} {
		if _, err := parse(tc.control...); err != nil {
			t.Errorf("control Parse(%q): %v", tc.control, err)
		}
		_, err := parse(tc.args...)
		var f ParseFailure
		if !errors.As(err, &f) || f.code != tc.code || f.Class() != reason.ClassUsage || !errors.Is(err, tc.want) {
			t.Errorf("Parse(%q) failed with %v (code %d); want a usage-class failure with code %d, %v", tc.args, err, f.code, tc.code, tc.want)
		}
	}
}

func TestEveryParseCodeHasItsOwnWireName(t *testing.T) {
	seen := map[string]ParseCode{}
	for _, code := range []ParseCode{UnknownVerb, Usage} {
		name := refuseParse(code, errUnknownVerb).Reason()
		if prior, ok := seen[name]; ok || name == "" {
			t.Errorf("codes %d and %d share the wire name %q", prior, code, name)
		}
		seen[name] = code
	}
}

func TestHelpNamesEveryRow(t *testing.T) {
	help := HelpText(Verbs(), Groups(), ActiveFields{Table: fields(t)})
	tables := map[string]Table{"": Verbs()}
	for _, g := range Groups() {
		tables[g.Word+" "] = g.Table
	}
	for prefix, table := range tables {
		for _, r := range table.rows {
			if !strings.Contains(help, prefix+r.Name) || !strings.Contains(help, r.Summary) {
				t.Errorf("--help does not name %q with its summary %q", prefix+r.Name, r.Summary)
			}
		}
	}
}

// TestHelpsFieldsAreTheTablesRows: the field section, read back as
// (place, name, kind, verbs), is the table's rows at each place with the
// verbs that write each, compared as sets.
func TestHelpsFieldsAreTheTablesRows(t *testing.T) {
	table := fields(t)
	help := HelpText(Verbs(), Groups(), ActiveFields{Table: table})
	got := map[string]bool{}
	place := ""
	for _, l := range strings.Split(help, "\n") {
		if strings.HasPrefix(l, "fields of ") {
			place = strings.TrimSuffix(strings.TrimPrefix(l, "fields of "), ":")
			continue
		}
		if place == "" || !strings.HasPrefix(l, "  ") {
			continue
		}
		cols := strings.Fields(l)
		got[place+"|"+cols[0]+"|"+cols[1]+"|"+strings.Join(cols[2:], " ")] = true
	}
	want := map[string]bool{}
	for _, p := range table.Places() {
		for _, r := range table.Rows(p) {
			writers := strings.Join(verbs.VerbsOf(r), ", ")
			if no := verbs.Unwritten(r); no != nil {
				writers = no.Reason()
			}
			want[config.PlaceName(p)+"|"+string(r.Path())+"|"+kindOf(r)+"|"+writers] = true
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("help's fields = %v\nwant the table's %v", got, want)
	}
}

// The table is the parser: one more row parses and is helped with no other
// edit `[LAW:composability]`.
func TestOneMoreRowIsParsedAndHelped(t *testing.T) {
	show := Row{Name: "show", Operands: TakesPaths{Make: func(p []PathText, _ Flags) Command { return Get{paths: p} }}, Summary: "read, by another name"}
	table := Table{rows: append(Verbs().rows, show)}
	got, err := Parse(table, Groups(), []string{"show", "x"})
	if err != nil || !reflect.DeepEqual(got, Get{paths: []PathText{"x"}}) {
		t.Errorf("show x = %#v, %v; want the new row's command", got, err)
	}
	if help := HelpText(table, Groups(), ActiveFields{Table: fields(t)}); !strings.Contains(help, "show") || !strings.Contains(help, show.Summary) {
		t.Errorf("--help does not name the new row:\n%s", help)
	}
}

// TestHelpWithNoVersionSaysWhy: with no version in force there is no field
// table to print, and help says why in its place.
func TestHelpWithNoVersionSaysWhy(t *testing.T) {
	help := HelpText(Verbs(), Groups(), NoFields{Why: "no version in force"})
	if strings.Contains(help, "fields of ") || !strings.Contains(help, "fields: no version in force") {
		t.Errorf("help with no version:\n%s", help)
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
