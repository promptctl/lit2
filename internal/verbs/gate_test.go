package verbs_test

import (
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/verbs"
)

// story is a feature in unfiled and a story under it, under b's file.
func story(t *testing.T, b *book) (feature, story string) {
	t.Helper()
	feature = filed(t, b.do(verbs.Add, op("unfiled/children", "{title: f}")))
	return feature, filed(t, b.do(verbs.Add, op(feature+"/children", "{title: s}")))
}

// TestAMoveUnderTheDefaultProfile walks one story through the shipped
// lists: off the graph refused naming where the column is entered from;
// out of the backlog refused, every unmet ready test named at once, until
// the story is groomed; into impl refused on the plan's verdict, which no
// review gives at this slice; and closed through a universal edge whose
// entry the same command's reason meets.
func TestAMoveUnderTheDefaultProfile(t *testing.T) {
	b := newBook(t, string(assets.DefaultWorkspace.Bytes()))
	feature, s := story(t, b)
	if got := b.refusal(verbs.Set, op(s+"/status", "impl")); got != "edge: "+s+"/status backlog → impl: impl is entered only from plan; nothing written" {
		t.Errorf("a move off the graph is refused as %q", got)
	}
	got := b.refusal(verbs.Set, op(s+"/status", "plan"))
	for _, want := range []string{
		"gate: " + s + "/status backlog → plan refused by backlog/exit; nothing written\n",
		"  error    backlog/exit  description (ready)  found (none)\n",
		"  error    backlog/exit  labels=ready (ready)  found (none) on " + s + " and its ancestors",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("an ungroomed story's move is refused as %q; want it to hold %q", got, want)
		}
	}
	if strings.Contains(got, "labels!=") || strings.Contains(got, "blockers") {
		t.Errorf("the refusal %q names a requirement that holds", got)
	}
	b.do(verbs.Set, op(s+"/description", "what and why"))
	b.do(verbs.Add, op(feature+"/labels", "ready"))
	if echo := b.do(verbs.Set, op(s+"/status", "plan")); echo != s+"/status backlog → plan\n  requirements: 5 evaluated · 5 met · 0 unmet (warning) · 0 unmet (error)\n" {
		t.Errorf("a groomed story's move echoes %q", echo)
	}
	if got := b.refusal(verbs.Set, op(s+"/status", "impl")); !strings.Contains(got, "refused by impl/entry") || !strings.Contains(got, "plan/verdict=pass  found (none)") {
		t.Errorf("a move into impl with no plan verdict is refused as %q", got)
	}
	if got := b.refusal(verbs.Set, op(s+"/status", "wontfix")); !strings.Contains(got, s+"/status plan → wontfix refused by wontfix/entry") {
		t.Errorf("closing with no reason is refused as %q", got)
	}
	if echo := b.do(verbs.Set, op(s+"/status", `{column: wontfix, reason: "not needed"}`)); !strings.Contains(echo, s+"/status plan → wontfix\n  "+s+"/status/reason = \"not needed\"\n  requirements: 1 evaluated · 1 met") {
		t.Errorf("closing with a reason in the move echoes %q", echo)
	}
	if got := b.get(s + "/stage"); got != "terminal\n" {
		t.Errorf("a closed story's stage reads %q", got)
	}
}

// TestAnUnmetWarningPrintsAndTheMoveLands: a warning on the destination's
// entry rides on the echo and refuses nothing.
func TestAnUnmetWarningPrintsAndTheMoveLands(t *testing.T) {
	b := newBook(t, strings.Replace(profile(""), "[columns.plan]\n", "[columns.plan]\nentry = [{ test = \"status/reason\", severity = \"warning\" }]\n", 1))
	_, s := story(t, b)
	want := s + "/status backlog → plan\n  warning  plan/entry  status/reason  found (none)\n  requirements: 1 evaluated · 0 met · 1 unmet (warning) · 0 unmet (error)\n"
	if echo := b.do(verbs.Set, op(s+"/status", "plan")); echo != want {
		t.Errorf("a move past a warning echoes %q; want %q", echo, want)
	}
}

// TestAFilingIsAnArrivalInTheInitialColumn: the initial column's entry
// list judges a new leaf, and never a node, which is in no column.
func TestAFilingIsAnArrivalInTheInitialColumn(t *testing.T) {
	b := newBook(t, strings.Replace(profile(""), "[columns.backlog]\n", "[columns.backlog]\nentry = [{ test = \"description\", severity = \"error\" }]\n", 1))
	feature := filed(t, b.do(verbs.Add, op("unfiled/children", "{title: f}")))
	if got := b.refusal(verbs.Add, op(feature+"/children", "{title: s}")); !strings.HasPrefix(got, "gate: (new 1) filed into backlog refused by backlog/entry") {
		t.Errorf("a story filed with no description is refused as %q; want it named as no store holds it", got)
	}
	if echo := b.do(verbs.Add, op(feature+"/children", "{title: s, description: d}")); !strings.Contains(echo, "requirements: 1 evaluated · 1 met") {
		t.Errorf("a story filed with a description echoes %q", echo)
	}
}

// TestANegativeAncestorsTestNamesTheAncestorThatBrokeIt: the line of an
// unmet labels!= names the value found where it was found, not on the
// item judged.
func TestANegativeAncestorsTestNamesTheAncestorThatBrokeIt(t *testing.T) {
	b := newBook(t, string(assets.DefaultWorkspace.Bytes()))
	feature, s := story(t, b)
	b.do(verbs.Add, op(feature+"/labels", "needs-design"))
	got := b.refusal(verbs.Set, op(s+"/status", "plan"))
	if want := "labels!=needs-design (ready)  found needs-design on " + feature + "\n"; !strings.Contains(got+"\n", want) {
		t.Errorf("a story under a needs-design feature is refused as %q; want it to hold %q", got, want)
	}
}

// TestATemplatesStatusTheGateRefusesIsTheTemplates: a move a level
// template gives is refused naming the template, as compile names it for
// any other value a template gives.
func TestATemplatesStatusTheGateRefusesIsTheTemplates(t *testing.T) {
	b := newBook(t, strings.Replace(profile(""), "name = \"story\"\n", "name = \"story\"\n[levels.1.template]\nstatus = \"done\"\n", 1))
	feature := filed(t, b.do(verbs.Add, op("unfiled/children", "{title: f}")))
	if got := b.refusal(verbs.Add, op(feature+"/children", "{title: s}")); !strings.HasPrefix(got, "template: level story's template sets status: (new 1)/status backlog → done: done is entered only from plan") {
		t.Errorf("a template's move off the graph is refused as %q", got)
	}
}

// TestSettlingAConflictedStatusIsNoMove:two concurrent moves leave the
// status two live writes; setting the column it reads settles them and
// takes no edge, though no next leads from that column to itself.
func TestSettlingAConflictedStatusIsNoMove(t *testing.T) {
	b := newBook(t, strings.Replace(profile(""), "[columns.plan]\nnext = [\"done\"]\n", "[columns.plan]\nnext = [\"review\", \"done\"]\n\n[columns.review]\nnext = [\"done\"]\n", 1))
	_, s := story(t, b)
	b.do(verbs.Set, op(s+"/status", "plan"))
	s0 := b.st
	b.as(one, s0, verbs.Set, op(s+"/status", "review"))
	b.as(two, s0, verbs.Set, op(s+"/status", "done"))
	reads := strings.TrimSuffix(b.get(s+"/status"), "\n")
	if echo := b.do(verbs.Set, op(s+"/status", reads)); echo != s+"/status "+reads+" → "+reads+"\n" {
		t.Errorf("settling the conflict at %s echoes %q", reads, echo)
	}
	if echo := b.do(verbs.Set, op(s+"/status", reads)); echo != "" {
		t.Errorf("after the settle, the same column drafts again: %q", echo)
	}
}

// TestAnUndeclaredColumnStandsAndIsReported:a status a later version no
// longer declares reads marked, its stage is active, and it leaves by a
// universal edge and by no other (scenarios.md S-6).
func TestAnUndeclaredColumnStandsAndIsReported(t *testing.T) {
	b := newBook(t, profile(""))
	_, s := story(t, b)
	b.mint(one, b.st, []events.Draft{events.Act{About: events.Item{ID: b.created},
		Payload: events.Transition{From: events.MustName("backlog"), To: events.MustName("audit")}}})
	if got := b.get(s + "/status"); got != "audit (undeclared)\n" {
		t.Errorf("an undeclared status reads %q", got)
	}
	if got := b.get(s + "/stage"); got != "active\n" {
		t.Errorf("an undeclared column's stage reads %q", got)
	}
	if got := b.refused(verbs.Set, op(s+"/status", "done")); got != "edge" {
		t.Errorf("a move from an undeclared column along no edge is refused as %s", got)
	}
	if echo := b.do(verbs.Set, op(s+"/status", "backlog")); echo != s+"/status audit (undeclared) → backlog\n" {
		t.Errorf("the way out echoes %q", echo)
	}
}

// TestANodesStageAndRollupAreItsLeaves: a node's stage and rollup are
// computed on read from the leaves under it, a removed one counting for
// nothing.
func TestANodesStageAndRollupAreItsLeaves(t *testing.T) {
	b := newBook(t, profile(""))
	feature := filed(t, b.do(verbs.Add, op("unfiled/children", "{title: f}")))
	if got := b.get(feature + "/stage"); got != "initial\n" {
		t.Errorf("an empty node's stage reads %q", got)
	}
	a := filed(t, b.do(verbs.Add, op(feature+"/children", "{title: a}")))
	c := filed(t, b.do(verbs.Add, op(feature+"/children", "{title: c}")))
	b.do(verbs.Set, op(a+"/status", "plan"))
	if got := b.get(feature + "/stage"); got != "active\n" {
		t.Errorf("a node with a leaf started reads %q", got)
	}
	if got := b.get(feature + "/rollup"); got != "backlog 1 · plan 1\n" {
		t.Errorf("the rollup reads %q", got)
	}
	b.do(verbs.Set, op(a+"/status", "done"))
	b.do(verbs.Rm, op(c, ""))
	if got := b.get(feature + "/stage"); got != "terminal\n" {
		t.Errorf("a node whose live leaves are done reads %q", got)
	}
	if got := b.get(feature + "/rollup"); got != "done 1\n" {
		t.Errorf("the rollup with a removed leaf reads %q", got)
	}
}
