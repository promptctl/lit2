package root

import (
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/assets"
)

// contributorBeside is s's store with a second checkout beside it, whose
// principal the owner has made a contributor: another member, on another
// replica of the same store.
func contributorBeside(s session) session {
	beside := NewTwinBeside(*s.twin)
	joiner := session{t: s.t, twin: &beside}
	joiner.ok("admin", "init")
	s.ok("add", "/members", "{principal: \""+besideKey().Principal().String()+"\", role: contributor}")
	return joiner
}

// TestAContributorClosesAnotherMembersTicketByMovingIt: closing is a move
// whose value carries what the column's entry needs, so contributor's
// `set */status` closes the owner's tickets into every terminal column,
// and the owner's checkout folds each close and reads its field.
func TestAContributorClosesAnotherMembersTicketByMovingIt(t *testing.T) {
	s := newSession(t)
	joiner := contributorBeside(s)
	feature := filed(t, s.ok("add", "unfiled/children", "{title: f}"))
	story := func() string { return filed(t, s.ok("add", feature+"/children", "{title: s}")) }
	obsolete, wontfix, dup, superseded := story(), story(), story(), story()
	if r := joiner.refused("unauthorized", "set", obsolete+"/description", "mine now"); !strings.Contains(r.err, "role contributor") {
		t.Fatalf("the contributor's write of the owner's description: stderr %q; want it refused naming the role, as set own/* allows", r.err)
	}
	s.ok("set", obsolete+"/description", "the owner's") // the control: the owner may
	joiner.ok("set", obsolete+"/status", `{column: obsolete, reason: "the board replaced it"}`)
	joiner.ok("set", wontfix+"/status", `{column: wontfix, reason: "not needed"}`)
	joiner.ok("set", dup+"/status", "{column: dup, of: "+obsolete+"}")
	joiner.ok("set", superseded+"/status", "{column: superseded, of: "+wontfix+"}")
	for path, want := range map[string]string{
		obsolete + "/status":          "obsolete\n",
		obsolete + "/status/reason":   "the board replaced it\n",
		wontfix + "/status/reason":    "not needed\n",
		dup + "/status":               "dup\n",
		dup + "/status/of":            obsolete + "\n",
		superseded + "/status/of":     wontfix + "\n",
		superseded + "/stage":         "terminal\n",
		obsolete + "/status/of":       "",
		superseded + "/status/reason": "",
	} {
		if got := s.ok("get", path); got != want {
			t.Errorf("the owner's get %s prints %q; want %q", path, got, want)
		}
	}
	if got := s.ok("get", "/integrity"); strings.Contains(got, "unauthorized") {
		t.Errorf("the owner's /integrity is %q; want no close carried", got)
	}
}

// TestATransitionFieldIsItsMovesAlone: a reason is written only by the
// move it explains, so it cannot be changed apart from one, a move into
// the column the ticket is in says it again, and a reopen takes it away.
func TestATransitionFieldIsItsMovesAlone(t *testing.T) {
	s := newSession(t)
	joiner := contributorBeside(s)
	feature := filed(t, s.ok("add", "unfiled/children", "{title: f}"))
	id := filed(t, s.ok("add", feature+"/children", "{title: s}"))
	s.ok("set", id+"/status", `{column: obsolete, reason: "first"}`)
	if r := joiner.refused("on_transition", "set", id+"/status/reason", "rewritten"); appends(r.event) != 0 || !strings.Contains(r.err, "a move writes it") {
		t.Errorf("a bare write of the reason: stderr %q, %d appends; want refused, naming the move", r.err, appends(r.event))
	}
	joiner.refused("on_transition", "add", id+"/status/reason", "rewritten")
	if echo := joiner.ok("set", id+"/status", `{column: obsolete, reason: "second"}`); !strings.Contains(echo, id+"/status obsolete → obsolete\n  requirements: 1 evaluated · 1 met · 0 unmet (warning) · 0 unmet (error)\n"+id+"/status/reason \"first\" → \"second\"\n") {
		t.Errorf("moving into the column it is in, with a reason, echoes %q", echo)
	}
	if got := s.ok("get", id+"/status/reason"); got != "second\n" {
		t.Errorf("after the second close the reason reads %q", got)
	}
	s.ok("set", id+"/status", "backlog")
	if got := s.ok("get", id+"/status/reason"); got != "" {
		t.Errorf("a reopened ticket's reason reads %q; want the reopen to have replaced it", got)
	}
	if r := s.refused("gate", "set", id+"/status", "wontfix"); !strings.Contains(r.err, "status/reason  found (none)") {
		t.Errorf("closing the reopened ticket with no reason is refused as %q", r.err)
	}
	// plan is entered only from backlog, and staying in it takes no edge
	s.ok("set", id+"/description", "what and why")
	s.ok("add", feature+"/labels", "ready")
	s.ok("set", id+"/status", "plan")
	if r := s.lit("set", id+"/status", `{column: plan, reason: "split from the board"}`); r.status != 0 {
		t.Errorf("a move with a field into the column the ticket is in: status %d, stderr %q", r.status, r.err)
	}
}

// TestATerminalColumnWithANewFieldNeedsNoRoleEdit: a workspace that adds
// a terminal column whose entry needs a field no column used declares
// the field below status, and a contributor closes into it with the
// roles the store was founded with.
func TestATerminalColumnWithANewFieldNeedsNoRoleEdit(t *testing.T) {
	s := newSession(t)
	joiner := contributorBeside(s)
	next := append(assets.DefaultWorkspace.Bytes(), `
[levels.1.fields."status/resolution"]
kind = "enum"
values = ["declined", "deferred"]

[columns.declined]
terminal = true
entry = [{ test = "status/resolution", severity = "error" }]
`...)
	s.file(next)
	s.commit(next)
	s.ok("config", "commit")
	feature := filed(t, s.ok("add", "unfiled/children", "{title: f}"))
	id := filed(t, s.ok("add", feature+"/children", "{title: s}"))
	joiner.refused("gate", "set", id+"/status", "declined")
	joiner.refused("value", "set", id+"/status", "{column: declined, resolution: maybe}")
	joiner.ok("set", id+"/status", "{column: declined, resolution: deferred}")
	if got := s.ok("get", id+"/status/resolution"); got != "deferred\n" {
		t.Errorf("the owner reads the declined ticket's resolution as %q", got)
	}
}
