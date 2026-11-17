package config

import (
	"slices"
	"testing"
)

// TestReachFollowsNextAlone: a column some next names is entered only
// along those edges, one no next names from every column, a recorded name
// the graph does not declare included.
func TestReachFollowsNextAlone(t *testing.T) {
	s, err := parseText(t, `prefix = "lit"
claim_ttl = "6h"

[levels.1]
name = "story"

[columns.backlog]
initial = true
next = ["plan"]

[columns.plan]
next = ["done"]

[columns.done]
terminal = true

[columns.wontfix]
terminal = true
`)
	if err != nil {
		t.Fatal(err)
	}
	eff, err := s.Effective(nil)
	if err != nil {
		t.Fatal(err)
	}
	g := eff.Columns
	for _, c := range []struct {
		from, to string
		edge     Edge
		ok       bool
	}{
		{"backlog", "plan", Forward, true},
		{"plan", "done", Forward, true},
		{"backlog", "done", 0, false},
		{"plan", "backlog", Universal, true},
		{"done", "wontfix", Universal, true},
		{"audit", "backlog", Universal, true},
		{"audit", "plan", 0, false},
	} {
		if edge, ok := g.Reach(c.from, c.to); edge != c.edge || ok != c.ok {
			t.Errorf("Reach(%s, %s) = %d, %t; want %d, %t", c.from, c.to, edge, ok, c.edge, c.ok)
		}
	}
	if got := g.EnteredFrom("done"); !slices.Equal(got, []string{"plan"}) {
		t.Errorf("done is entered from %v; want plan", got)
	}
}
