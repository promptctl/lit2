package cli

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/commandname"
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

// parse is the command args parse to on lit2's command line.
func parse(args ...string) (Command, error) {
	p, err := Parse(Lit2(), args)
	return p.Command, err
}

// operandsFor is one operand line o takes: a path, a pair, or nothing.
func operandsFor(o Operands) []string {
	switch o.(type) {
	case ReadsPaths, WritesPaths:
		return []string{"x"}
	case WritesPairs:
		return []string{"x", "v"}
	case TakesNothing:
		return nil
	}
	panic(fmt.Sprintf("%T is not Operands", o))
}

// made is the command r's own Make makes of operandsFor, with no flags.
func made(r Row) Command {
	switch o := r.Operands.(type) {
	case ReadsPaths:
		return o.Make([]PathText{"x"})
	case WritesPaths:
		return o.Make([]PathText{"x"}, Flags{})
	case WritesPairs:
		return o.Make([]Pair{{"x", "v"}}, Flags{})
	case TakesNothing:
		return o.Make()
	}
	panic(fmt.Sprintf("%T is not Operands", r.Operands))
}

// Every row of lit2's command line parses, by its name, to the command
// its own Make makes, asked under that name, and --help names it with its
// summary: a row added, renamed or regrouped is covered with no edit here.
func TestEveryRowParsesToItsCommandUnderItsName(t *testing.T) {
	c := Lit2()
	help := HelpText(c, ActiveFields{Table: fields(t)})
	for _, r := range c.rows {
		args := append(r.Name.Words(), operandsFor(r.Operands)...)
		p, err := Parse(c, args)
		if err != nil || p.Asked.Name != r.Name || !reflect.DeepEqual(p.Command, made(r)) {
			t.Errorf("Parse(%q) = %#v, %v; want %#v asked as %s", args, p, err, made(r), r.Name)
		}
		if !helpLine(help, r) {
			t.Errorf("--help has no line of %s with its summary %q", r.Name, r.Summary)
		}
	}
}

// helpLine is whether help has r's own line: its name first, its summary
// last.
func helpLine(help string, r Row) bool {
	for _, l := range strings.Split(help, "\n") {
		if strings.HasPrefix(l, "  "+r.Name.String()+" ") && strings.HasSuffix(l, " "+r.Summary) {
			return true
		}
	}
	return false
}

func TestOperandsAndFlagsParseIntoTheCommand(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want Command
	}{
		{[]string{"get", "x", "y/title"}, Get{paths: []PathText{"x", "y/title"}}},
		{[]string{"set", "x/status", "done", "y/title", "two words"}, Set{pairs: []Pair{{"x/status", "done"}, {"y/title", "two words"}}}},
		{[]string{"rm", "x", "--dry-run"}, Rm{paths: []PathText{"x"}, flags: Flags{DryRun: true}}},
		{[]string{"--dry-run", "add", "x/labels", "ready"}, Add{pairs: []Pair{{"x/labels", "ready"}}, flags: Flags{DryRun: true}}},
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
		want          error
	}{
		{[]string{"frobnicate"}, []string{"get", "x"}, errUnknownVerb},
		{[]string{"admin", "frobnicate"}, []string{"admin", "init"}, errUnknownVerb},
		{[]string{"get"}, []string{"get", "x"}, errNoPaths},
		{[]string{"set", "x/status"}, []string{"set", "x/status", "v"}, errNotPairs},
		{[]string{"add", "x/labels", "a", "x/labels"}, []string{"add", "x/labels", "a"}, errNotPairs},
		{[]string{"admin"}, []string{"admin", "init"}, errNoCommand},
		{[]string{"config"}, []string{"config", "commit"}, errNoCommand},
		{[]string{"config", "frobnicate"}, []string{"config", "commit"}, errUnknownVerb},
		{[]string{"config", "commit", "--dry-run"}, []string{"config", "commit"}, errNoDryRun},
		{[]string{"admin", "init", "extra"}, []string{"admin", "init"}, errOperands},
		{[]string{"get", "x", "--frob"}, []string{"get", "x"}, errUnknownFlag},
		{[]string{"get", "x", "--dry-run"}, []string{"get", "x"}, errNoDryRun},
		{[]string{"admin", "init", "--dry-run"}, []string{"admin", "init"}, errNoDryRun},
	} {
		if _, err := parse(tc.control...); err != nil {
			t.Errorf("control Parse(%q): %v", tc.control, err)
		}
		_, err := parse(tc.args...)
		var f ParseFailure
		if !errors.As(err, &f) || f.Class() != reason.ClassUsage || !errors.Is(err, tc.want) {
			t.Errorf("Parse(%q) failed with %v; want a usage-class failure of %v", tc.args, err, tc.want)
		}
	}
}

// TestEveryParseRefusalHasItsOwnWireName: a word that names no row is
// unknown_verb, and a row's operands or a flag that are wrong are usage.
func TestEveryParseRefusalHasItsOwnWireName(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"frobnicate"}, "unknown_verb"},
		{[]string{"config", "frobnicate"}, "unknown_verb"},
		{[]string{"get"}, "usage"},
		{[]string{"get", "x", "--frob"}, "usage"},
		{[]string{"config", "commit", "--dry-run"}, "usage"},
	} {
		_, err := parse(tc.args...)
		var f ParseFailure
		if !errors.As(err, &f) {
			t.Errorf("Parse(%q) = %v; want a ParseFailure", tc.args, err)
			continue
		}
		if got := f.Reason(); got != tc.want {
			t.Errorf("Parse(%q) is reason %q; want %q", tc.args, got, tc.want)
		}
	}
}

// TestHelpsFieldsAreTheTablesRows: the field section, read back as
// (place, name, kind, verbs), is the table's rows at each place with the
// verbs that write each, compared as sets.
func TestHelpsFieldsAreTheTablesRows(t *testing.T) {
	table := fields(t)
	help := HelpText(Lit2(), ActiveFields{Table: table})
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
func TestOneMoreRowIsParsedHelpedAndAskedUnderItsName(t *testing.T) {
	c := Lit2()
	show := Row{Name: commandname.Verb("show"), Operands: ReadsPaths{Make: func(p []PathText) Command { return Get{paths: p} }}, Summary: "read, by another name"}
	// a write, which takes --dry-run by its shape alone
	erase := Row{Name: commandname.Verb("erase"), Operands: WritesPaths{Make: func(p []PathText, f Flags) Command { return Rm{paths: p, flags: f} }}, Summary: "delete, by another name"}
	// a command of a kind nothing else in the package has heard of
	ping := Row{Name: commandname.InGroup("admin", "ping"), Operands: TakesNothing{Make: func() Command { return probe{} }}, Summary: "a command no switch knows"}
	more, err := NewCommands(append(slices.Clone(c.rows), show, erase, ping), c.bare)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(more, []string{"show", "x"})
	if err != nil || got.Asked.Name != show.Name || !reflect.DeepEqual(got.Command, Get{paths: []PathText{"x"}}) {
		t.Errorf("show x = %#v, %v; want a get asked as show", got, err)
	}
	got, err = Parse(more, []string{"erase", "x", "--dry-run"})
	if err != nil || got.Asked.Name != erase.Name || !reflect.DeepEqual(got.Command, Rm{paths: []PathText{"x"}, flags: Flags{DryRun: true}}) {
		t.Errorf("erase x --dry-run = %#v, %v; want a dry rm asked as erase", got, err)
	}
	if _, err := Parse(more, []string{"show", "x", "--dry-run"}); !errors.Is(err, errNoDryRun) {
		t.Errorf("show x --dry-run = %v; want errNoDryRun: a read takes no --dry-run", err)
	}
	got, err = Parse(more, []string{"admin", "ping"})
	if err != nil || got.Asked.Name != ping.Name || got.Command != (probe{}) {
		t.Errorf("admin ping = %#v, %v; want the new kind asked as admin ping", got, err)
	}
	if help := HelpText(more, ActiveFields{Table: fields(t)}); !strings.Contains(help, show.Summary) || !strings.Contains(help, "admin ping") {
		t.Errorf("--help does not name the new rows:\n%s", help)
	}
}

// probe is a command of a kind only this test makes.
type probe struct{}

func (probe) command() {}

// A command line a word line could parse two ways, or that names a row
// the log keeps for no row's, or runs bare what does not parse, is not
// built; each beside lit2's own, which is.
func TestALineThatDoesNotParseOneWayIsNotBuilt(t *testing.T) {
	c := Lit2()
	nothing := TakesNothing{Make: func() Command { return Init{} }}
	row := func(n commandname.Name) Row { return Row{Name: n, Operands: nothing} }
	for _, tc := range []struct {
		name string
		rows []Row
		bare Bare
		want error
	}{
		{"a row twice", []Row{row(commandname.Get())}, c.bare, errAmbiguous},
		{"a verb a group's word", []Row{row(commandname.Verb("admin"))}, c.bare, errAmbiguous},
		{"a word with a space", []Row{row(commandname.Verb("two words"))}, c.bare, errNotAWord},
		{"a word scan takes for a flag", []Row{row(commandname.Verb("--x"))}, c.bare, errNotAWord},
		{"an empty group", []Row{row(commandname.InGroup("", "sync"))}, c.bare, errNotAWord},
		{"no operand shape", []Row{{Name: commandname.Verb("ping")}}, c.bare, errNoMake},
		{"no Make", []Row{{Name: commandname.Verb("ping"), Operands: TakesNothing{}}}, c.bare, errNoMake},
		{"an empty word", []Row{row(commandname.InGroup("admin", ""))}, c.bare, errNotAWord},
		{"the unparsed name", []Row{row(commandname.Unparsed())}, c.bare, errReservedName},
		{"the help name", []Row{row(commandname.Help())}, c.bare, errReservedName},
		{"the forged name", []Row{row(commandname.Forged())}, c.bare, errReservedName},
		{"a bare of no row", nil, Bare{Name: commandname.Verb("show")}, errBareUnrouted},
		{"a bare its row refuses", nil, Bare{Name: commandname.Get()}, errBareUnrouted},
		{"a bare with operands its row takes none of", nil, Bare{Name: commandname.Init(), Operands: []string{"x"}}, errBareUnrouted},
		{"a bare with an operand scan reads as a flag", nil, Bare{Name: commandname.Get(), Operands: []string{"--help"}}, errBareNotAsTyped},
		{"a bare with the end of the flags", nil, Bare{Name: commandname.Get(), Operands: []string{"--", "x"}}, errBareNotAsTyped},
	} {
		if _, err := NewCommands(append(slices.Clone(c.rows), tc.rows...), tc.bare); !errors.Is(err, tc.want) {
			t.Errorf("%s: NewCommands = %v; want %v", tc.name, err, tc.want)
		}
	}
	// the group's word reached first by a verb is the same ambiguity
	if _, err := NewCommands(append([]Row{row(commandname.Verb("admin"))}, c.rows...), c.bare); !errors.Is(err, errAmbiguous) {
		t.Errorf("a verb before the group of its word: NewCommands = %v; want %v", err, errAmbiguous)
	}
}

// The paths the event records, and the command's, are the paths as
// typed, whatever a body does with what the command hands out.
func TestTheSummaryKeepsThePathsAsTyped(t *testing.T) {
	c := Lit2()
	for _, r := range c.rows {
		p, err := Parse(c, append(r.Name.Words(), operandsFor(r.Operands)...))
		if err != nil {
			t.Fatal(err)
		}
		paths, takesPaths := p.Command.(interface{ Paths() []PathText })
		pairs, takesPairs := p.Command.(interface{ Pairs() []Pair })
		if takesPaths {
			paths.Paths()[0] = "changed"
		}
		if takesPairs {
			pairs.Pairs()[0].Path = "changed"
		}
		if !takesPaths && !takesPairs {
			continue
		}
		if !slices.Equal(p.Asked.Paths, []PathText{"x"}) {
			t.Errorf("%s: the summary's paths are %v after the command's changed; want [x], as typed", r.Name, p.Asked.Paths)
		}
		if !reflect.DeepEqual(p.Command, made(r)) {
			t.Errorf("%s: the command is %#v after what it handed out changed; want %#v", r.Name, p.Command, made(r))
		}
	}
}

// The prose around the rows is rendered from them: the usage line names
// every group, the bare line the row a bare lit2 runs, and both flags,
// in --help and in the unknown flag's refusal.
func TestTheProseAroundTheRowsFollowsThem(t *testing.T) {
	c := Lit2()
	read := commandname.Verb("read")
	rows := slices.Clone(c.rows)
	for i, r := range rows {
		if r.Name == commandname.Get() {
			rows[i].Name = read
		}
	}
	rows = append(rows, Row{Name: commandname.InGroup("team", "list"), Operands: TakesNothing{Make: func() Command { return Sync{} }}})
	renamed, err := NewCommands(rows, Bare{Name: read, Operands: []string{"/"}, Shows: "everything"})
	if err != nil {
		t.Fatal(err)
	}
	help := HelpText(renamed, NoFields{Why: "none"})
	for _, want := range []string{"lit2 admin <command>", "lit2 config <command>", "lit2 team <command>", "lit2 alone is read /: everything", helpFlag, dryRunFlag} {
		if !strings.Contains(help, want) {
			t.Errorf("--help does not say %q:\n%s", want, help)
		}
	}
	if p, err := Parse(renamed, nil); err != nil || p.Asked.Name != read {
		t.Errorf("a bare lit2 = %#v, %v; want the renamed row", p, err)
	}
	_, err = Parse(renamed, []string{"read", "x", "--frob"})
	if !errors.Is(err, errUnknownFlag) || !strings.Contains(err.Error(), helpFlag) || !strings.Contains(err.Error(), dryRunFlag) {
		t.Errorf("an unknown flag is refused with %v; want it to list %s and %s", err, helpFlag, dryRunFlag)
	}
}

// TestHelpWithNoVersionSaysWhy: with no version in force there is no field
// table to print, and help says why in its place.
func TestHelpWithNoVersionSaysWhy(t *testing.T) {
	help := HelpText(Lit2(), NoFields{Why: "no version in force"})
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
	p, err := Parse(Lit2(), []string{"set", "x/status", "done", "y/title", "secret words"})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Asked; got.Name != commandname.Set() || !reflect.DeepEqual(got.Paths, []PathText{"x/status", "y/title"}) || got.Values != 2 {
		t.Errorf("set is asked as %#v; want set, both paths, two values", got)
	}
}

// Every flag scan takes is said by --help, with what it does, and listed
// by the refusal of a flag it does not take.
func TestHelpAndTheUnknownFlagSayEveryFlag(t *testing.T) {
	help := HelpText(Lit2(), NoFields{Why: "none"})
	_, err := parse("get", "x", "--frob")
	for _, f := range theFlags() {
		if !strings.Contains(help, f.name+" "+f.does) {
			t.Errorf("--help does not say %s %s", f.name, f.does)
		}
		if !errors.Is(err, errUnknownFlag) || !strings.Contains(err.Error(), f.name) {
			t.Errorf("an unknown flag is refused with %v; want it to list %s", err, f.name)
		}
	}
}

// A command line holds what it checked: changing the rows or the bare
// operands it was built from, or the bare it hands out, changes nothing
// it parses.
func TestACommandLineHoldsWhatItChecked(t *testing.T) {
	rows, bare := Lit2().Rows(), Lit2().Bare()
	c, err := NewCommands(rows, bare)
	if err != nil {
		t.Fatal(err)
	}
	rows[0].Name = commandname.Verb("admin")
	bare.Operands[0] = "changed"
	c.Bare().Operands[0] = "changed"
	if p, err := Parse(c, []string{"admin", "init"}); err != nil || p.Asked.Name != commandname.Init() {
		t.Errorf("admin init after its rows changed = %#v, %v; want admin init", p, err)
	}
	if p, err := Parse(c, nil); err != nil || !slices.Equal(p.Asked.Paths, []PathText{"/"}) {
		t.Errorf("a bare lit2 after its operands changed = %#v, %v; want get /", p, err)
	}
}
