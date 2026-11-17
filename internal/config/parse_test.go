package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/field"
	"github.com/promptctl/lit2/internal/git"
)

// running is the reader every parse test reads under: a release new enough for
// every gate the fixtures write.
var running = ReaderVersion{v: "v0.4.0"}

// minimal is the smallest file that parses: a prefix, a claim lifetime, two
// levels, and a graph of one initial and one terminal column. Each refusal
// case is a minimal edit of it.
const minimal = `prefix = "lit"
claim_ttl = "6h"

[levels.1]
name = "story"

[levels.2]
name = "feature"
home = "unfiled"

[columns.backlog]
initial = true
next = ["done"]

[columns.done]
terminal = true
`

// parseText parses a fixture under the test reader.
func parseText(t *testing.T, text string) (Schema, error) {
	t.Helper()
	return Parse([]byte(text), "fixture", running)
}

// refusalsOf is the refusals a fixture produced, failing t when it parsed.
func refusalsOf(t *testing.T, text string) []Refusal {
	t.Helper()
	_, err := parseText(t, text)
	var r *Refused
	if !errors.As(err, &r) {
		t.Fatalf("expected a refusal, got %v", err)
	}
	return r.Refusals
}

// TestDefaultProfileParsesToTheDocumentedModel holds the shipped default to
// what the design documents say it ships.
func TestDefaultProfileParsesToTheDocumentedModel(t *testing.T) {
	s, err := Load(NeverCommitted{}, running)
	if err != nil {
		t.Fatal(err)
	}
	sc := s.Schema()
	if sc.Prefix().String() != "lit" {
		t.Errorf("prefix = %s, want lit", sc.Prefix())
	}
	if _, ok := sc.Gate().(NoGate); !ok {
		t.Errorf("gate = %T, want NoGate", sc.Gate())
	}
	wantLevels := []struct{ name, home string }{{"story", ""}, {"feature", "unfiled"}, {"epic", "unassigned"}}
	for i, want := range wantLevels {
		lv, ok := sc.Level(i + 1)
		if !ok || lv.Name != want.name || lv.Home != want.home {
			t.Errorf("level %d = %+v, want %s home %q", i+1, lv, want.name, want.home)
		}
	}
	if _, ok := sc.Level(4); ok {
		t.Error("the default declares three levels, not four")
	}
	if got := sc.Labels().Values(); strings.Join(got, ",") != "ready,needs-design,external" {
		t.Errorf("labels = %v", got)
	}
	ready := sc.Checks()["ready"]
	if len(ready.Tests) != 5 {
		t.Fatalf("ready holds %d tests, want the five v3 put on backlog's exit", len(ready.Tests))
	}
	for _, tst := range ready.Tests {
		if tst.DeclaredIn != "ready" {
			t.Errorf("test %s is declared in %q, want ready", tst.Atom, tst.DeclaredIn)
		}
	}
	g := sc.Root().Columns
	backlog, ok := g.Resolve("backlog")
	if !ok || backlog.Stage() != Initial {
		t.Fatalf("backlog = %v, want the initial column", backlog)
	}
	if got := g.Initial().Name(); got != "backlog" {
		t.Errorf("initial = %s", got)
	}
	if len(backlog.Exit()) != 5 || backlog.Exit()[0].Severity != Error {
		t.Errorf("backlog's exit = %+v, want ready as five tests at error", backlog.Exit())
	}
	chain := []string{"backlog", "plan", "impl", "review", "merge", "done"}
	for i, name := range chain[:len(chain)-1] {
		c, _ := g.Resolve(name)
		if len(c.Next()) != 1 || c.Next()[0] != chain[i+1] {
			t.Errorf("%s.next = %v, want %s", name, c.Next(), chain[i+1])
		}
	}
	for _, name := range []string{"done", "dup", "superseded", "obsolete", "wontfix"} {
		c, ok := g.Resolve(name)
		if !ok || c.Stage() != Terminal {
			t.Errorf("%s is not terminal", name)
		}
	}
	if impl, _ := g.Resolve("impl"); len(impl.Entry()) != 1 || impl.Entry()[0].Severity != Error {
		t.Errorf("impl's entry = %+v, want plan/verdict=pass at error", impl.Entry())
	}
	if review, _ := g.Resolve("review"); review.Entry()[0].Severity != Warning {
		t.Errorf("review's entry severity = %v, want warning", review.Entry()[0].Severity)
	}
	if merge, _ := g.Resolve("merge"); merge.Entry()[0].Test.Atom.String() != "pr/verdict=pass" {
		t.Errorf("merge's entry = %s", merge.Entry()[0].Test.Atom)
	}
	if dup, _ := g.Resolve("dup"); dup.Entry()[0].Test.Atom.String() != "of" {
		t.Errorf("dup's entry = %s, want of", dup.Entry()[0].Test.Atom)
	}
	if obs, _ := g.Resolve("obsolete"); obs.Entry()[0].Test.Atom.String() != "reason" {
		t.Errorf("obsolete's entry = %s, want reason", obs.Entry()[0].Test.Atom)
	}
	if links := sc.Links(); links["blocks"].Kind != Dependency || len(links["blocks"].Between) != 3 {
		t.Errorf("blocks = %+v", links["blocks"])
	}
	if links := sc.Links(); links["relates"].Kind != Relates || len(links["relates"].Between) != 9 {
		t.Errorf("relates = %+v", links["relates"])
	}
	if ttl := sc.Root().ClaimTTL.String(); ttl != "6h0m0s" {
		t.Errorf("claim_ttl = %s, want 6h", ttl)
	}
}

// TestNoCommittedVersionReadsAsTheDefault holds the two sources to one parse:
// a workspace with no committed version reads the default, and the config-ref
// is the id git gives the default's bytes (architecture.md §3.15, Δ8).
func TestNoCommittedVersionReadsAsTheDefault(t *testing.T) {
	def, err := Load(NeverCommitted{}, running)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := def.Source().(FromDefault); !ok {
		t.Errorf("source = %T, want FromDefault", def.Source())
	}
	want := git.HashBlob(git.SHA1, assets.DefaultWorkspace.Bytes())
	if got := def.Ref(git.SHA1); got != want {
		t.Errorf("ref = %s, want the default's blob id %s", got, want)
	}
	logged, err := Load(Committed{Bytes: []byte(minimal)}, running)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := logged.Source().(FromLog); !ok {
		t.Errorf("source = %T, want FromLog", logged.Source())
	}
	if logged.Ref(git.SHA1) != git.HashBlob(git.SHA1, []byte(minimal)) {
		t.Error("a committed version's ref is the id git gives its bytes")
	}
}

// TestRefusalsNameTheirReasonAndKey is one row per reason token: each row is a
// minimal edit of the minimal file that the named reason refuses, and the
// refusal carries the key path that holds the fault.
func TestRefusalsNameTheirReasonAndKey(t *testing.T) {
	cases := []struct {
		name   string
		edit   func(string) string
		reason Reason
	}{
		{"syntax", func(s string) string { return s + "\n[levels.\n" }, ReasonSyntax},
		{"unknown-key", func(s string) string { return s + "\n[workflows]\nx = 1\n" }, ReasonUnknownKey},
		{"missing-key", replace(`prefix = "lit"`, ""), ReasonMissingKey},
		{"wrong-type", replace(`prefix = "lit"`, `prefix = 3`), ReasonWrongType},
		{"reader-too-old", replace(`prefix = "lit"`, "prefix = \"lit\"\nmin-reader-version = \"9.0.0\""), ReasonReaderTooOld},
		{"bad-version", replace(`prefix = "lit"`, "prefix = \"lit\"\nmin-reader-version = \"nope\""), ReasonBadVersion},
		{"bad-prefix", replace(`prefix = "lit"`, `prefix = "Lit"`), ReasonBadPrefix},
		{"no-levels", replace("[levels.1]\nname = \"story\"\n\n[levels.2]\nname = \"feature\"\nhome = \"unfiled\"\n", ""), ReasonNoLevels},
		{"level-out-of-range", replace("[levels.2]", "[levels.6]"), ReasonLevelOutOfRange},
		{"levels-not-contiguous", replace("[levels.2]", "[levels.3]"), ReasonLevelsNotContiguous},
		{"empty-level-name", replace(`name = "story"`, `name = ""`), ReasonEmptyLevelName},
		{"duplicate-level-name", replace(`name = "feature"`, `name = "story"`), ReasonDuplicateLevelName},
		{"home-on-leaf", replace("[levels.1]\nname = \"story\"", "[levels.1]\nname = \"story\"\nhome = \"x\""), ReasonHomeOnLeaf},
		{"missing-home", replace(`home = "unfiled"`, ""), ReasonMissingHome},
		{"bad-home", replace(`home = "unfiled"`, `home = "Unfiled"`), ReasonBadHome},
		{"duplicate-home", replace("[columns.backlog]", "[levels.3]\nname = \"epic\"\nhome = \"unfiled\"\n\n[columns.backlog]"), ReasonDuplicateHome},
		{"home-collides", replace(`home = "unfiled"`, `home = "story"`), ReasonHomeCollides},
		{"bad-field-name", replace(`name = "story"`, "name = \"story\"\n[levels.1.fields.Bad]\nkind = \"text\""), ReasonBadFieldName},
		{"unknown-kind", replace(`name = "story"`, "name = \"story\"\n[levels.1.fields.pts]\nkind = \"color\""), ReasonUnknownKind},
		{"kind-parameter", replace(`name = "story"`, "name = \"story\"\n[levels.1.fields.pts]\nkind = \"text\"\nvalues = [\"a\"]"), ReasonKindParameter},
		{"builtin-redeclared", replace(`name = "story"`, "name = \"story\"\n[levels.1.fields.description]\nkind = \"text\""), ReasonBuiltinRedeclared},
		{"bad-label", replace(`prefix = "lit"`, "prefix = \"lit\"\nlabels = [\"Bad Label\"]"), ReasonBadLabel},
		{"duplicate-label", replace(`prefix = "lit"`, "prefix = \"lit\"\nlabels = [\"a\", \"a\"]"), ReasonDuplicateLabel},
		{"alias-collides", replace("[columns.done]\nterminal = true", "[columns.done]\nterminal = true\naliases = [\"backlog\"]"), ReasonAliasCollides},
		{"duplicate-alias", replace("[columns.done]\nterminal = true", "[columns.done]\nterminal = true\naliases = [\"x\"]\n\n[columns.plan]\nnext = [\"done\"]\naliases = [\"x\"]"), ReasonDuplicateAlias},
		{"template-field-undeclared", replace(`name = "story"`, "name = \"story\"\n[levels.1.template]\nnope = \"x\""), ReasonTemplateFieldUndeclared},
		{"template-field-derived", replace(`name = "story"`, "name = \"story\"\n[levels.1.template]\n\"plan/verdict\" = \"pass\""), ReasonTemplateFieldDerived},
		{"template-value", replace(`name = "story"`, "name = \"story\"\n[levels.1.fields.pts]\nkind = \"number\"\n[levels.1.template]\npts = \"abc\""), ReasonTemplateValue},
		{"template-member-twice", func(s string) string {
			return replace(`prefix = "lit"`, "prefix = \"lit\"\nlabels = [\"a\"]")(replace(`name = "story"`, "name = \"story\"\n[levels.1.template]\nlabels = [\"a\", \"a\"]")(s))
		}, ReasonTemplateValue},
		{"template-field-not-keyable", replace(`name = "story"`, "name = \"story\"\n[levels.1.template]\ncomments = \"x\""), ReasonTemplateFieldNotKeyable},
		{"template-field-not-on-level", replace(`home = "unfiled"`, "home = \"unfiled\"\n[levels.2.template]\nstatus = \"backlog\""), ReasonTemplateFieldUndeclared},
		{"many-enum-value-not-a-name", replace(`name = "story"`, "name = \"story\"\n[levels.1.fields.area]\nkind = \"enum\"\nmany = true\nvalues = [\"Front End\"]"), ReasonKindParameter},
		{"bad-column-name", replace("[columns.done]", "[columns.Done]"), ReasonBadColumnName},
		{"initial-and-terminal", replace("[columns.done]\nterminal = true", "[columns.done]\nterminal = true\ninitial = true"), ReasonInitialAndTerm},
		{"exit-on-terminal", replace("[columns.done]\nterminal = true", "[columns.done]\nterminal = true\nexit = []"), ReasonExitOnTerminal},
		{"next-on-terminal", replace("[columns.done]\nterminal = true", "[columns.done]\nterminal = true\nnext = []"), ReasonNextOnTerminal},
		{"next-target-undeclared", replace(`next = ["done"]`, `next = ["nowhere"]`), ReasonNextUndeclared},
		{"no-initial", replace("initial = true\n", ""), ReasonNoInitial},
		{"many-initial", replace("[columns.done]", "[columns.plan]\ninitial = true\nnext = [\"done\"]\n\n[columns.done]"), ReasonManyInitial},
		{"no-terminal", replace("terminal = true", ""), ReasonNoTerminal},
		{"bad-atom", replace("[columns.done]\nterminal = true", "[columns.done]\nterminal = true\nentry = [{ test = \"=bad\", severity = \"error\" }]"), ReasonBadAtom},
		{"path-undeclared", replace("[columns.done]\nterminal = true", "[columns.done]\nterminal = true\nentry = [{ test = \"nope\", severity = \"error\" }]"), ReasonPathUndeclared},
		{"path-is-check", replace("[columns.done]\nterminal = true", "[checks.ready]\ntests = [{ test = \"description\" }]\n\n[columns.done]\nterminal = true\nentry = [{ test = \"ready\", severity = \"error\" }]"), ReasonPathIsCheck},
		{"atom-value", func(s string) string {
			s = replace(`prefix = "lit"`, "prefix = \"lit\"\nlabels = [\"ready\"]")(s)
			return replace("[columns.done]\nterminal = true", "[columns.done]\nterminal = true\nentry = [{ test = \"labels=nope\", severity = \"error\" }]")(s)
		}, ReasonAtomValue},
		{"bad-scope", replace("[columns.done]\nterminal = true", "[columns.done]\nterminal = true\nentry = [{ test = \"description\", scope = \"everywhere\", severity = \"error\" }]"), ReasonBadScope},
		{"bad-severity", replace("[columns.done]\nterminal = true", "[columns.done]\nterminal = true\nentry = [{ test = \"description\" }]"), ReasonBadSeverity},
		{"test-or-check", replace("[columns.done]\nterminal = true", "[columns.done]\nterminal = true\nentry = [{ test = \"description\", check = \"x\", severity = \"error\" }]"), ReasonTestOrCheck},
		{"bad-check-name", replace("[columns.done]", "[checks.Ready]\ntests = []\n\n[columns.done]"), ReasonBadCheckName},
		{"check-undeclared", replace("[columns.done]\nterminal = true", "[columns.done]\nterminal = true\nentry = [{ check = \"ghost\", severity = \"error\" }]"), ReasonCheckUndeclared},
		{"check-cycle", replace("[columns.done]", "[checks.a]\ntests = [{ check = \"b\" }]\n\n[checks.b]\ntests = [{ check = \"a\" }]\n\n[columns.done]"), ReasonCheckCycle},
		{"check-collides", replace("[columns.done]", "[checks.description]\ntests = []\n\n[columns.done]"), ReasonCheckCollides},
		{"bad-link-name", replace("[columns.backlog]", "[links.Blocks]\nkind = \"relates\"\nbetween = [[1, 1]]\n\n[columns.backlog]"), ReasonBadLinkName},
		{"bad-link-kind", replace("[columns.backlog]", "[links.x]\nkind = \"blah\"\nbetween = [[1, 1]]\n\n[columns.backlog]"), ReasonBadLinkKind},
		{"empty-between", replace("[columns.backlog]", "[links.x]\nkind = \"relates\"\nbetween = []\n\n[columns.backlog]"), ReasonEmptyBetween},
		{"bad-pair", replace("[columns.backlog]", "[links.x]\nkind = \"relates\"\nbetween = [[1]]\n\n[columns.backlog]"), ReasonBadPair},
		{"pair-level-undeclared", replace("[columns.backlog]", "[links.x]\nkind = \"relates\"\nbetween = [[1, 9]]\n\n[columns.backlog]"), ReasonPairLevelUndeclared},
		{"bad-duration", replace(`claim_ttl = "6h"`, `claim_ttl = "soon"`), ReasonBadDuration},
		{"bad-node-key", replace("[columns.backlog]", "[nodes.zzz]\nclaim_ttl = \"1h\"\n\n[columns.backlog]"), ReasonBadNodeKey},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := tc.edit(minimal)
			if text == minimal {
				t.Fatal("the edit changed nothing: the minimal file no longer holds the text it edits")
			}
			refusals := refusalsOf(t, text)
			for _, r := range refusals {
				if r.Reason == tc.reason {
					if r.Key == "" && tc.reason != ReasonSyntax {
						t.Errorf("%s refused with no key path: %s", tc.reason, r.Message)
					}
					return
				}
			}
			t.Errorf("want %s; got %v", tc.reason, refusals)
		})
	}
}

// TestEveryFaultIsNamedAtOnce: a file with three faults refuses naming all
// three, each with its key path, never only the first.
func TestEveryFaultIsNamedAtOnce(t *testing.T) {
	text := replace(`prefix = "lit"`, `prefix = "Lit"`)(minimal)
	text = replace(`claim_ttl = "6h"`, `claim_ttl = "soon"`)(text)
	text = replace(`next = ["done"]`, `next = ["nowhere"]`)(text)
	refusals := refusalsOf(t, text)
	for _, key := range []KeyPath{"prefix", "claim_ttl", "columns.backlog.next"} {
		found := false
		for _, r := range refusals {
			if r.Key == key {
				found = true
			}
		}
		if !found {
			t.Errorf("no refusal names %s; got %v", key, refusals)
		}
	}
}

// fullFile uses every section the workspace file has, so it is the one fixture
// that must parse end to end.
const fullFile = `min-reader-version = "0.4.0"
prefix = "lit"
claim_ttl = "6h"
labels = ["ready", "needs-design"]

[levels.1]
name = "story"

[levels.1.fields.points]
kind = "number"

[levels.1.fields.size]
kind = "enum"
values = ["s", "m", "l"]
many = true

[levels.1.fields.ticket]
kind = "pattern"
pattern = "^T-[0-9]+$"

[levels.1.fields.opened]
kind = "date"

[levels.1.template]
size = ["m"]
points = "3"

[levels.2]
name = "feature"
home = "unfiled"

[levels.3]
name = "epic"
home = "unassigned"

[checks.ready]
tests = [{ test = "description" }, { test = "labels=ready", scope = "ancestors" }]

[checks.sized]
tests = [{ check = "ready" }, { test = "size", scope = "self" }]

[links.blocks]
kind = "dependency"
between = [[1, 1], [2, 2], [3, 3]]

[links.relates]
kind = "relates"
between = [[1, 2]]

[columns.backlog]
initial = true
next = ["plan"]
aliases = ["inbox"]
exit = [{ check = "ready", severity = "error" }]

[columns.plan]
next = ["done"]
entry = [{ test = "points", severity = "warning" }]

[columns.done]
terminal = true
entry = [{ check = "sized", severity = "error" }]

[nodes.unfiled]
claim_ttl = "2h"

[nodes.k7k20000000000000000000000.columns.review]
next = ["audit"]
entry = [{ test = "labels=needs-design", scope = "ancestors", severity = "warning" }]

[nodes.unfiled.columns.audit]
next = ["done"]
`

// TestEverySectionParses: a workspace file using every section produces the
// typed schema.
func TestEverySectionParses(t *testing.T) {
	s, err := parseText(t, fullFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(s.Checks()["sized"].Tests); got != 3 {
		t.Errorf("sized flattens to %d tests, want ready's two and size", got)
	}
	if s.Checks()["sized"].Tests[0].DeclaredIn != "ready" {
		t.Errorf("a test from an included check keeps its declaring name: got %q", s.Checks()["sized"].Tests[0].DeclaredIn)
	}
	if r, err := s.Table().Find(field.Leaf{}, "size"); err != nil || r.(FieldRow).Decl.Kind == nil {
		t.Errorf("a declared field is a row at its level's items: %v", err)
	}
	// unfiled is one section though two tables name it; the item is the other.
	if len(s.Sections()) != 2 {
		t.Errorf("sections = %d, want 2", len(s.Sections()))
	}
}

// TestOneLevelFileHasNoHome: a one-level workspace is a flat list of leaves.
func TestOneLevelFileHasNoHome(t *testing.T) {
	text := replace("[levels.2]\nname = \"feature\"\nhome = \"unfiled\"\n", "")(minimal)
	s, err := parseText(t, text)
	if err != nil {
		t.Fatal(err)
	}
	lv, ok := s.Level(1)
	if !ok || lv.Name != "story" || lv.Home != "" {
		t.Errorf("the leaf = %+v, want story with no home", lv)
	}
}

// TestLoadRefusesWhatTheReaderCannotRead: a gate above the reader stops the
// version, naming the upgrade.
func TestLoadRefusesWhatTheReaderCannotRead(t *testing.T) {
	gated := replace(`prefix = "lit"`, "prefix = \"lit\"\nmin-reader-version = \"9.0.0\"")(minimal)
	_, err := Load(Committed{Bytes: []byte(gated)}, running)
	var r *Refused
	if !errors.As(err, &r) || r.Refusals[0].Reason != ReasonReaderTooOld {
		t.Fatalf("want reader-too-old, got %v", err)
	}
	if !strings.Contains(r.Refusals[0].Message, "upgrade") {
		t.Errorf("the refusal names the upgrade: %q", r.Refusals[0].Message)
	}
}

// TestChecksAndColumnsCarryTheirSeverity: a column entry naming a check gives
// each of the check's tests the entry's severity and the check's name.
func TestChecksAndColumnsCarryTheirSeverity(t *testing.T) {
	text := replace("[columns.done]\nterminal = true", "[checks.ready]\ntests = [{ test = \"description\" }]\n\n[columns.done]\nterminal = true\nentry = [{ check = \"ready\", severity = \"warning\" }]")(minimal)
	s, err := parseText(t, text)
	if err != nil {
		t.Fatal(err)
	}
	done, _ := s.Root().Columns.Resolve("done")
	if len(done.Entry()) != 1 {
		t.Fatalf("done's entry = %v, want the check's one test", done.Entry())
	}
	if got := done.Entry()[0]; got.Severity != Warning || got.Test.DeclaredIn != "ready" {
		t.Errorf("entry = %+v, want severity warning declared in ready", got)
	}
}

// TestAliasesResolveToTheirColumn: a renamed column keeps its old name as an
// alias, and a recorded name resolves through it.
func TestAliasesResolveToTheirColumn(t *testing.T) {
	text := replace("[columns.done]\nterminal = true", "[columns.review]\nnext = [\"done\"]\naliases = [\"qa\"]\n\n[columns.done]\nterminal = true")(minimal)
	text = replace(`next = ["done"]`, `next = ["review"]`)(text)
	s, err := parseText(t, text)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := s.Root().Columns.Resolve("qa")
	if !ok || c.Name() != "review" {
		t.Errorf("qa resolves to %v, want review", c)
	}
	if _, ok := s.Root().Columns.Resolve("nowhere"); ok {
		t.Error("a name neither declared nor aliased resolves to nothing")
	}
}

// TestAReaderTooOldSeesOnlyTheGate: a version gated above the reader is
// refused for the gate alone, not for the keys this reader does not know.
func TestAReaderTooOldSeesOnlyTheGate(t *testing.T) {
	text := replace(`prefix = "lit"`, "prefix = \"lit\"\nmin-reader-version = \"9.0.0\"\nworkflows = \"a key from 9.0.0\"")(minimal)
	refusals := refusalsOf(t, text)
	if len(refusals) != 1 || refusals[0].Reason != ReasonReaderTooOld {
		t.Errorf("refusals = %v, want reader-too-old alone", refusals)
	}
}

// TestTheSameNameOnTwoLevelsIsTwoFields: each level may declare a field of
// one name with its own kind, and a test's value is held to every one.
func TestTheSameNameOnTwoLevelsIsTwoFields(t *testing.T) {
	text := replace(`name = "story"`, "name = \"story\"\n[levels.1.fields.points]\nkind = \"number\"")(minimal)
	text = replace(`home = "unfiled"`, "home = \"unfiled\"\n[levels.2.fields.points]\nkind = \"enum\"\nvalues = [\"1\", \"3\"]")(text)
	held := replace("[columns.done]\nterminal = true", "[columns.done]\nterminal = true\nentry = [{ test = \"points=3\", severity = \"error\" }]")(text)
	s, err := parseText(t, held)
	if err != nil {
		t.Fatal(err)
	}
	two, _ := events.ParseLevel(2)
	leaf, _ := s.Table().Find(field.Leaf{}, "points")
	feature, _ := s.Table().Find(field.Node{Level: two}, "points")
	if _, ok := leaf.(FieldRow).Decl.Kind.(field.Number); !ok {
		t.Errorf("level 1's points = %#v, want a number", leaf)
	}
	if _, ok := feature.(FieldRow).Decl.Kind.(field.Enum); !ok {
		t.Errorf("level 2's points = %#v, want an enum", feature)
	}
	refusals := refusalsOf(t, replace("points=3", "points=2")(held))
	if len(refusals) != 1 || refusals[0].Reason != ReasonAtomValue {
		t.Errorf("points=2 fits level 1's number and not level 2's enum: refusals = %v, want atom-value", refusals)
	}
}

// TestAColumnCheckReferenceTakesNoScope: a check's tests carry their own
// scope, so a scope on the reference is refused on a column as in a check.
func TestAColumnCheckReferenceTakesNoScope(t *testing.T) {
	text := replace("[columns.done]\nterminal = true", "[checks.ready]\ntests = [{ test = \"description\" }]\n\n[columns.done]\nterminal = true\nentry = [{ check = \"ready\", scope = \"ancestors\", severity = \"error\" }]")(minimal)
	refusals := refusalsOf(t, text)
	if len(refusals) != 1 || refusals[0].Key != "columns.done.entry[0].scope" {
		t.Errorf("refusals = %v, want one at columns.done.entry[0].scope", refusals)
	}
}

// TestAFaultInACheckIsNamedOnce: a check with a bad test is refused once, at
// the test, however many checks and columns name it, and nothing that names
// it is told it names no check.
func TestAFaultInACheckIsNamedOnce(t *testing.T) {
	text := replace("[columns.done]\nterminal = true", "[checks.a]\ntests = [{ check = \"b\" }]\n\n[checks.b]\ntests = [{ test = \"nope\" }]\n\n[columns.done]\nterminal = true\nentry = [{ check = \"b\", severity = \"error\" }]")(minimal)
	refusals := refusalsOf(t, text)
	if len(refusals) != 1 || refusals[0].Reason != ReasonPathUndeclared || refusals[0].Key != "checks.b.tests[0].test" {
		t.Errorf("refusals = %v, want path-undeclared once, at checks.b.tests[0].test", refusals)
	}
	cycle := replace("[columns.done]", "[checks.a]\ntests = [{ check = \"b\" }]\n\n[checks.b]\ntests = [{ check = \"a\" }]\n\n[columns.done]")(minimal)
	if refusals := refusalsOf(t, cycle); len(refusals) != 1 || refusals[0].Reason != ReasonCheckCycle {
		t.Errorf("refusals = %v, want the cycle once", refusals)
	}
}

// TestATestIsAString: a test that is not a string is the wrong type, not an
// atom that failed to parse.
func TestATestIsAString(t *testing.T) {
	text := replace("[columns.done]\nterminal = true", "[columns.done]\nterminal = true\nentry = [{ test = 5, severity = \"error\" }]")(minimal)
	refusals := refusalsOf(t, text)
	if len(refusals) != 1 || refusals[0].Reason != ReasonWrongType {
		t.Errorf("refusals = %v, want wrong-type", refusals)
	}
}

// TestASectionHasOneKeyPath: every refusal in a home's section names the
// section the way the file writes it.
func TestASectionHasOneKeyPath(t *testing.T) {
	text := replace("[columns.backlog]", "[nodes.unfiled.columns.Bad]\nterminal = true\n\n[nodes.unfiled.columns.audit]\nnext = [\"nowhere\"]\n\n[columns.backlog]")(minimal)
	refusals := refusalsOf(t, text)
	if len(refusals) != 2 {
		t.Fatalf("refusals = %v, want the bad name and the bad next", refusals)
	}
	for _, r := range refusals {
		if !strings.HasPrefix(string(r.Key), "nodes.unfiled.columns.") {
			t.Errorf("%s refused at %s, want under nodes.unfiled.columns", r.Reason, r.Key)
		}
	}
}

// TestARefusedHomeNamesItsLevel: a level's refusal names it by its number,
// whatever levels around it are missing.
func TestARefusedHomeNamesItsLevel(t *testing.T) {
	text := replace("[levels.2]\nname = \"feature\"", "[levels.3]\nname = \"epic\"\nhome = \"unfiled\"\n\n[levels.2]\nname = \"feature\"")(minimal)
	text = replace("[levels.2]\nname = \"feature\"\nhome = \"unfiled\"\n", "")(text)
	text = replace("[levels.3]\nname = \"epic\"\nhome = \"unfiled\"\n", "[levels.3]\nname = \"epic\"\nhome = \"story\"\n")(text)
	refusals := refusalsOf(t, text)
	for _, r := range refusals {
		if r.Reason == ReasonHomeCollides {
			if r.Key != "levels.3.home" {
				t.Errorf("home-collides at %s, want levels.3.home", r.Key)
			}
			return
		}
	}
	t.Errorf("refusals = %v, want home-collides", refusals)
}

// TestFactsCountEveryRequirement: the count of requirements covers the
// sections' columns as well as the root's.
func TestFactsCountEveryRequirement(t *testing.T) {
	text := replace("[columns.backlog]", "[nodes.unfiled.columns.audit]\nnext = [\"done\"]\nentry = [{ test = \"description\", severity = \"error\" }]\n\n[columns.backlog]")(minimal)
	l, err := Load(Committed{Bytes: []byte(text)}, running)
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Facts().Counts.Requirements; got != 1 {
		t.Errorf("requirements = %d, want the section's one", got)
	}
}

// TestTheRootIsAGraphAlone: a root column whose next only a section declares
// is refused, since an item under no section would have no such column.
func TestTheRootIsAGraphAlone(t *testing.T) {
	text := replace(`next = ["done"]`, `next = ["audit"]`)(minimal)
	text = replace("[columns.backlog]", "[nodes.unfiled.columns.audit]\nnext = [\"done\"]\n\n[columns.backlog]")(text)
	refusals := refusalsOf(t, text)
	if len(refusals) != 1 || refusals[0].Key != "columns.backlog.next" || refusals[0].Reason != ReasonNextUndeclared {
		t.Errorf("refusals = %v, want next-target-undeclared at columns.backlog.next", refusals)
	}
}

// TestEachFaultOnce: a key of the wrong type is refused for its type, and
// nothing else is said about the value it does not have.
func TestEachFaultOnce(t *testing.T) {
	cases := map[string]string{
		"a link kind that is not a string": replace("[columns.backlog]", "[links.x]\nkind = 5\nbetween = [[1, 1]]\n\n[columns.backlog]")(minimal),
		"a between that is not a list":     replace("[columns.backlog]", "[links.x]\nkind = \"relates\"\nbetween = 5\n\n[columns.backlog]")(minimal),
		"enum values that are not a list":  replace(`name = "story"`, "name = \"story\"\n[levels.1.fields.pts]\nkind = \"enum\"\nvalues = \"a\"")(minimal),
	}
	for name, text := range cases {
		if refusals := refusalsOf(t, text); len(refusals) != 1 || refusals[0].Reason != ReasonWrongType {
			t.Errorf("%s: refusals = %v, want wrong-type alone", name, refusals)
		}
	}
	more := map[string]string{
		"a field kind that is not a string": replace(`name = "story"`, "name = \"story\"\n[levels.1.fields.f]\nkind = 5")(minimal),
		"a level name that is not a string": replace(`name = "story"`, `name = 5`)(minimal),
		"a severity that is not a string":   replace("[columns.done]\nterminal = true", "[columns.done]\nterminal = true\nentry = [{ test = \"description\", severity = 5 }]")(minimal),
		"enum values holding a number":      replace(`name = "story"`, "name = \"story\"\n[levels.1.fields.pts]\nkind = \"enum\"\nvalues = [1]")(minimal),
	}
	for name, text := range more {
		if refusals := refusalsOf(t, text); len(refusals) != 1 || refusals[0].Reason != ReasonWrongType {
			t.Errorf("%s: refusals = %v, want wrong-type alone", name, refusals)
		}
	}
	collides := replace("[columns.done]\nterminal = true", "[checks.description]\ntests = []\n\n[checks.ready]\ntests = [{ test = \"description\" }]\n\n[columns.done]\nterminal = true\nentry = [{ test = \"description\", severity = \"error\" }]")(minimal)
	if refusals := refusalsOf(t, collides); len(refusals) != 1 || refusals[0].Reason != ReasonCheckCollides {
		t.Errorf("a check named like a field: refusals = %v, want check-collides alone", refusals)
	}
	noColumns := minimal[:strings.Index(minimal, "[columns.backlog]")]
	for _, r := range refusalsOf(t, noColumns) {
		if r.Reason == ReasonMissingKey {
			t.Errorf("a file with no columns says so as the graph's faults, not also as missing-key: %v", r)
		}
	}
}

// TestAHomeIsAHomeWhateverItsName: a section keyed by a declared home is that
// home's, even when the name could also be read as an item id.
func TestAHomeIsAHomeWhateverItsName(t *testing.T) {
	text := replace(`home = "unfiled"`, `home = "`+anItem+`"`)(minimal)
	text = replace("[columns.backlog]", "[nodes."+anItem+"]\nclaim_ttl = \"1h\"\n\n[columns.backlog]")(text)
	s, err := parseText(t, text)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Sections()[HomeNode(anItem)]; !ok {
		t.Errorf("sections = %v, want the home %s's", s.Sections(), anItem)
	}
}

// anItem is an item's full id that is also a home's name.
const anItem = "k7k20000000000000000000000"

// TestAnItemKeyIsItsFullID: a section names its item by the full id, its one
// spelling; a short id or a prefixed one is refused, so two keys never name
// one item and a key's meaning survives every sync.
func TestAnItemKeyIsItsFullID(t *testing.T) {
	s, err := parseText(t, replace("[columns.backlog]", "[nodes."+anItem+"]\nclaim_ttl = \"1h\"\n\n[columns.backlog]")(minimal))
	if err != nil {
		t.Fatal(err)
	}
	id, err := events.ParseItemID(anItem)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Sections()[ItemNode(id)]; !ok {
		t.Errorf("sections = %v, want %s's", s.Sections(), anItem)
	}
	for _, key := range []string{"k7k", `"lit-k7k"`, `"lit-` + anItem + `"`} {
		refusals := refusalsOf(t, replace("[columns.backlog]", "[nodes."+key+"]\nclaim_ttl = \"1h\"\n\n[columns.backlog]")(minimal))
		if len(refusals) != 1 || refusals[0].Reason != ReasonBadNodeKey {
			t.Errorf("[nodes.%s]: refusals = %v, want one bad-node-key", key, refusals)
		}
	}
}

// TestAGateIsAFullVersion: min-reader-version is MAJOR.MINOR.PATCH, so one
// gate has one spelling in the log.
func TestAGateIsAFullVersion(t *testing.T) {
	for _, v := range []string{"9", "1.2", "1.2.3+build"} {
		text := replace(`prefix = "lit"`, "prefix = \"lit\"\nmin-reader-version = \""+v+"\"")(minimal)
		if refusals := refusalsOf(t, text); len(refusals) != 1 || refusals[0].Reason != ReasonBadVersion {
			t.Errorf("min-reader-version %s: refusals = %v, want bad-version", v, refusals)
		}
	}
}

// TestACheckCycleNamesItsPath: a cycle through other checks names each check
// on it.
func TestACheckCycleNamesItsPath(t *testing.T) {
	text := replace("[columns.done]", "[checks.a]\ntests = [{ check = \"b\" }]\n\n[checks.b]\ntests = [{ check = \"a\" }]\n\n[columns.done]")(minimal)
	refusals := refusalsOf(t, text)
	if len(refusals) != 1 || !strings.Contains(refusals[0].Message, "a -> b -> a") {
		t.Errorf("refusals = %v, want one naming a -> b -> a", refusals)
	}
}

// TestFactsCountSectionColumns: columns and requirements are counted over the
// same columns, the root's and the sections'.
func TestFactsCountSectionColumns(t *testing.T) {
	text := replace("[columns.backlog]", "[nodes.unfiled.columns.audit]\nnext = [\"done\"]\n\n[columns.backlog]")(minimal)
	l, err := Load(Committed{Bytes: []byte(text)}, running)
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Facts().Counts.Columns; got != 3 {
		t.Errorf("columns = %d, want the root's two and the section's one", got)
	}
}

// replace is one minimal edit: the first occurrence of old becomes new. A case
// whose old text is absent changes nothing, which the test reports.
func replace(old, new string) func(string) string {
	return func(s string) string {
		return strings.Replace(s, old, new, 1)
	}
}
