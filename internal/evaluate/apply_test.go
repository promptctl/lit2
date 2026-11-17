package evaluate

import (
	"strings"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
)

// version is a two-level workspace whose root columns are columns, with
// more appended: backlog → audit → done unless a test says otherwise.
func version(t *testing.T, columns, more string) config.Schema {
	t.Helper()
	text := "prefix = \"lit\"\nclaim_ttl = \"6h\"\n[levels.1]\nname = \"story\"\n[levels.2]\nname = \"feature\"\nhome = \"unfiled\"\n" + columns + more
	return must(config.Parse([]byte(text), "fixture", running))
}

const withAudit = `
[columns.backlog]
initial = true
next = ["audit"]
[columns.audit]
next = ["done"]
[columns.done]
terminal = true
`

const withoutAudit = `
[columns.backlog]
initial = true
next = ["done"]
[columns.done]
terminal = true
`

// inAudit is a state with one story moved into audit.
func inAudit(t *testing.T) (fold.State, events.ItemID) {
	s := founded
	s, story := mint(t, s, s.Known(), create(events.AtLevel{Level: must(events.ParseLevel(1))}), [16]byte{0x30})
	s, _ = mint(t, s, s.Known(), events.Act{About: events.Item{ID: story}, Payload: events.Transition{From: events.MustName("backlog"), To: events.MustName("audit")}}, [16]byte{})
	return s, story
}

func reasons(rs []config.Refusal) []config.Reason {
	var out []config.Reason
	for _, r := range rs {
		out = append(out, r.Reason)
	}
	return out
}

// A version that drops a column a ticket is in refuses, naming the column's
// key path, the ticket, and the aliases line that would say where it goes.
func TestDroppingAHeldColumnIsRefusedWithTheAliasesLine(t *testing.T) {
	s, story := inAudit(t)
	got := CheckApply(s, []config.Schema{version(t, withAudit, "")}, version(t, withoutAudit, ""))
	if len(got) != 1 || got[0].Reason != config.ReasonColumnHeld || got[0].Key != "columns.audit" {
		t.Fatalf("dropping audit while a story is in it is %v; want one column-held at columns.audit", got)
	}
	rendered := events.Render(must(events.ParsePrefix("lit")), story, s.Path(story))
	if m := got[0].Message; !strings.Contains(m, rendered) || !strings.Contains(m, "`columns.<column>.aliases = [\"audit\"]`") {
		t.Errorf("the refusal says %q; want the ticket %s and the aliases line", m, rendered)
	}
}

func TestAnAliasSayingWhereTheTicketsGoPasses(t *testing.T) {
	s, _ := inAudit(t)
	next := version(t, withoutAudit, "aliases = [\"audit\"]\n")
	if got := CheckApply(s, []config.Schema{version(t, withAudit, "")}, next); len(got) != 0 {
		t.Errorf("done aliasing audit is refused: %v", got)
	}
}

// A column no live version reads is S-6's undeclared column already: a
// version that also lacks it strands nothing new, so it passes, and so
// does the first version of a store with no version in force.
func TestAColumnNoLiveVersionReadsBlocksNothing(t *testing.T) {
	s, _ := inAudit(t)
	if got := CheckApply(s, []config.Schema{version(t, withoutAudit, "")}, version(t, withoutAudit, "")); len(got) != 0 {
		t.Errorf("a column already undeclared is refused: %v", got)
	}
	if got := CheckApply(s, nil, version(t, withoutAudit, "")); len(got) != 0 {
		t.Errorf("a first version is refused: %v", got)
	}
}

// Every live version guards its columns: a resolution that drops a column
// only the version it replaces declared is refused too.
func TestAColumnAContendingVersionDeclaresIsGuarded(t *testing.T) {
	s, _ := inAudit(t)
	live := []config.Schema{version(t, withoutAudit, ""), version(t, withAudit, "")}
	if got := reasons(CheckApply(s, live, version(t, withoutAudit, ""))); len(got) != 1 || got[0] != config.ReasonColumnHeld {
		t.Errorf("dropping a contending version's held column is %v; want column-held", got)
	}
}

func TestARemovedTicketHoldsNoColumn(t *testing.T) {
	s, story := inAudit(t)
	s, _ = mint(t, s, s.Known(), events.Act{About: events.Item{ID: story}, Payload: events.FieldSet{Field: events.MustName("deleted"), Value: events.Plain{Canonical: "true"}}}, [16]byte{})
	if got := CheckApply(s, []config.Schema{version(t, withAudit, "")}, version(t, withoutAudit, "")); len(got) != 0 {
		t.Errorf("a removed ticket's column is guarded: %v", got)
	}
}

// A ticket under a removed node is no work either, as every view reads it.
func TestATicketUnderARemovedNodeHoldsNoColumn(t *testing.T) {
	s := founded
	s, feature := mint(t, s, s.Known(), create(events.AtLevel{Level: must(events.ParseLevel(2))}), [16]byte{0x10})
	s, story := mint(t, s, s.Known(), create(events.BelowParent{Parent: feature}), [16]byte{0x30})
	s, _ = mint(t, s, s.Known(), events.Act{About: events.Item{ID: story}, Payload: events.Transition{From: events.MustName("backlog"), To: events.MustName("audit")}}, [16]byte{})
	s, _ = mint(t, s, s.Known(), events.Act{About: events.Item{ID: feature}, Payload: events.FieldSet{Field: events.MustName("deleted"), Value: events.Plain{Canonical: "true"}}}, [16]byte{1})
	if got := CheckApply(s, []config.Schema{version(t, withAudit, "")}, version(t, withoutAudit, "")); len(got) != 0 {
		t.Errorf("a column held under a removed node is guarded: %v", got)
	}
}

// An id typed under a prefix any applied version declared still parses
// after the prefix changes; a prefix no version declared does not.
func TestTheViewKnowsEveryPrefixAVersionDeclared(t *testing.T) {
	s := founded
	s, _ = mint(t, s, s.Known(), events.Act{About: events.Root{}, Payload: events.NewWorkspaceApply([]byte("prefix = \"old\"\n"), git.SHA1, nil)}, [16]byte{})
	v := Of(s, version(t, withoutAudit, ""), time.Time{})
	for text, ok := range map[string]bool{"lit-1f9": true, "old-1f9": true, "rin-1f9": false} {
		if _, err := events.ParseShortID(text, v.Prefixes()); (err == nil) != ok {
			t.Errorf("%s under the view's prefixes: %v; want read %v", text, err, ok)
		}
	}
}

// An item section whose item this store has not seen has no ancestry to be
// judged on here, so it refuses nothing: read alone, over the root, its
// next to a column only its home declares would refuse a valid version.
func TestAnUnseenItemsSectionIsNotJudgedAlone(t *testing.T) {
	more := "[nodes.unfiled.columns.qa]\nterminal = true\n[nodes.k7k20000000000000000000000.columns.audit]\nnext = [\"qa\"]\n"
	if got := CheckApply(fold.State{}, nil, version(t, withAudit, more)); len(got) != 0 {
		t.Errorf("CheckApply = %v; want none", got)
	}
}

// The stacked shapes that parse and then refuse every read under their
// ancestry are refused at apply, each with its key path. (A root next only
// a section declares is Parse's to refuse: the root graph stands alone.)
func TestAStackedGraphThatDoesNotResolveIsRefused(t *testing.T) {
	epic := "[levels.3]\nname = \"epic\"\nhome = \"unassigned\"\n"
	for _, c := range []struct {
		name, more string
		want       config.Reason
		key        config.KeyPath
	}{
		{"a section's second initial column", "[nodes.unfiled.columns.start]\ninitial = true\nnext = [\"done\"]\n", config.ReasonManyInitial, "nodes.unfiled.columns.start"},
		{"a section column aliasing a root column", "[nodes.unfiled.columns.end]\nterminal = true\naliases = [\"backlog\"]\n", config.ReasonAliasCollides, "nodes.unfiled.columns.end"},
		{"a section's next only another section declares", epic + "[nodes.unfiled.columns.audit]\nnext = [\"extra\"]\n[nodes.unassigned.columns.extra]\nterminal = true\n", config.ReasonNextUndeclared, "nodes.unfiled.columns.audit"},
	} {
		got := CheckApply(fold.State{}, nil, version(t, withoutAudit, c.more))
		if len(got) != 1 || got[0].Reason != c.want || !strings.HasPrefix(string(got[0].Key), string(c.key)) {
			t.Errorf("%s: CheckApply = %v; want one %s at %s", c.name, got, c.want, c.key)
		}
	}
}
