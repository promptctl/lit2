package verbs

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/evaluate"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/field"
	"github.com/promptctl/lit2/internal/fold"
)

// Judgment is one arrival's requirements judged: how many the lists held,
// and each that does not hold, in list order.
type Judgment struct {
	Evaluated int
	Unmet     []UnmetRequirement
}

// UnmetRequirement is one requirement of a column's list that does not
// hold, as its line prints it: its severity, the list that declared it,
// the test, and the value found on the item that decided it.
type UnmetRequirement struct {
	Severity config.Severity
	Side     string
	Test     config.Test
	Found    []string
	On       Who
}

// arrival is one item entering a column: by a move from the column it
// reads, or, with no from, by its filing into the initial one.
type arrival struct {
	who  Who
	id   events.ItemID
	from *string
	to   string
}

// Gate judges every arrival the command makes in w, the state its minted
// events fold onto, so a write the same command makes counts: the one
// enforcer of the column graph's edges and requirement lists
// `[LAW:single-enforcer]`. A move must follow an edge the graph has; the
// source's exit list guards a forward edge, and the destination's entry
// list every arrival, a leaf's filing into the initial column included.
// Every list is evaluated whole, so a refusal names everything unmet at
// once. An unmet error refuses the command and it writes nothing; an unmet
// warning rides on the echo, in the judgment the arriving change carries
// (transition-requirements.md).
func Gate(w World, c Compiled, minted []events.Event) Compiled {
	about := drafted(c.Drafts, minted)
	filed := map[int]events.ItemID{}
	created := map[events.ItemID]int{}
	for _, ch := range c.Changes {
		if f, ok := ch.(Filed); ok {
			id := about[f.Draft].(events.NewItem).ID
			filed[f.N], created[id] = id, f.N
		}
	}
	// an item the command files is named by its number, which every line
	// renders as the echo does `[LAW:one-source-of-truth]`
	whoOf := func(id events.ItemID) Who {
		if n, ok := created[id]; ok {
			return Created{N: n}
		}
		return Existing{At: events.Item{ID: id}, Rendered: w.render(id)}
	}
	item := func(x Who) events.ItemID {
		switch x := x.(type) {
		case Existing:
			return x.At.(events.Item).ID // only an item has a status
		case Created:
			return filed[x.N]
		}
		panic(fmt.Sprintf("verbs: %T is not a Who", x))
	}
	var errs []error
	refused := map[int]*refusal{}
	changes := slices.Clone(c.Changes)
	for i, ch := range changes {
		var r *refusal
		var op int
		switch ch := ch.(type) {
		case Moved:
			ch.Judged, r = judge(w, arrival{who: ch.Who, id: item(ch.Who), from: &ch.From, to: ch.To}, whoOf)
			changes[i], op = ch, ch.Op
		case Filed:
			id := filed[ch.N]
			if _, leaf := field.ItemPlace(w.item(id).Level.(fold.Leveled).Level).(field.Leaf); !leaf {
				continue // a node is in no column: its stage is its children's
			}
			ch.Judged, r = judge(w, arrival{who: Created{N: ch.N}, id: id}, whoOf)
			changes[i], op = ch, ch.Op
		case Wrote, Added, Removed, Commented:
			continue // no arrival
		}
		if r != nil {
			errs = append(errs, r.err)
			if refused[op] == nil {
				refused[op] = r
			}
		}
	}
	c.Changes = changes
	first := true
	for i, res := range c.Operands {
		r, ok := refused[i]
		if !ok {
			continue
		}
		err := r.err
		if first {
			err, first = errors.Join(errs...), false // the command's refusal lists every arrival refused
		}
		c.Operands[i] = Refused{Text: res.(Accepted).Text, Reason: r.reason, Err: err}
	}
	if len(refused) > 0 {
		c.Drafts, c.Changes = nil, nil
	}
	return c
}

// judge is one arrival judged: the edge it takes, then each requirement of
// the lists that edge carries. A refusal writes nothing, so it names every
// item as no store holds it yet.
func judge(w World, a arrival, whoOf func(events.ItemID) Who) (Judgment, *refusal) {
	at := unwritten(a.who)
	eff, err := effective(w.Schema(), evaluate.ItemAncestry(w.State(), w.Schema(), a.id))
	if err != nil {
		return Judgment{}, refuse("configuration", "%v", err)
	}
	g := eff.Columns
	to := g.Initial()
	move := fmt.Sprintf("%s filed into %s", at, to.Name())
	var lists [][]config.Requirement
	var sides []string
	if a.from != nil {
		col, _ := g.Resolve(a.to) // compile resolved it, and no write of a command changes a graph
		to, move = col, fmt.Sprintf("%s/status %s → %s", at, *a.from, col.Name())
		kind, reached := g.Reach(*a.from, col.Name())
		if !reached {
			return Judgment{}, refuse("edge", "%s: %s is entered only from %s; nothing written", move, col.Name(), strings.Join(g.EnteredFrom(col.Name()), ", "))
		}
		if kind == config.Forward {
			src, _ := g.Resolve(*a.from) // a forward edge leaves a declared column
			lists, sides = append(lists, src.Exit()), append(sides, src.Name()+"/exit")
		}
	}
	lists, sides = append(lists, to.Entry()), append(sides, to.Name()+"/entry")
	var j Judgment
	var blocked []string
	for k, reqs := range lists {
		for _, req := range reqs {
			unmet, err := Judge(w, a.id, []config.Test{req.Test})
			if err != nil {
				return Judgment{}, refuse("configuration", "%v", err)
			}
			j.Evaluated++
			for _, u := range unmet {
				j.Unmet = append(j.Unmet, UnmetRequirement{Severity: req.Severity, Side: sides[k], Test: u.Test, Found: u.Found, On: whoOf(u.On)})
				if req.Severity == config.Error && (len(blocked) == 0 || blocked[len(blocked)-1] != sides[k]) {
					blocked = append(blocked, sides[k])
				}
			}
		}
	}
	if len(blocked) == 0 {
		return j, nil
	}
	lines := []string{fmt.Sprintf("%s refused by %s; nothing written", move, strings.Join(blocked, ", "))}
	for _, u := range j.Unmet {
		lines = append(lines, u.line(unwritten))
	}
	return j, &refusal{reason: "gate", err: errors.New(strings.Join(lines, "\n"))}
}

// line is the requirement's line, its items named by name: severity,
// list, test and the check it was written in, then the value found. An
// ancestors test names where that value was read: the item that broke a
// negative test, or for a positive one, which no item held, the item
// judged and its ancestors.
func (r UnmetRequirement) line(name func(Who) string) string {
	severity := "error"
	if r.Severity == config.Warning {
		severity = "warning"
	}
	test := r.Test.Atom.String()
	if r.Test.DeclaredIn != "" {
		test += " (" + r.Test.DeclaredIn + ")"
	}
	found := "(none)"
	if len(r.Found) > 0 {
		found = strings.Join(r.Found, ", ")
	}
	if r.Test.Scope == config.Ancestors {
		found += " on " + name(r.On)
		if !negative(r.Test.Atom) {
			found += " and its ancestors"
		}
	}
	return fmt.Sprintf("  %-7s  %s  %s  found %s", severity, r.Side, test, found)
}

// echo is what a judgment adds under its change's line: each warning, an
// unmet error having refused the command, then the counts, and nothing
// when no list held a requirement.
func (j Judgment) echo(name func(Who) string) string {
	if j.Evaluated == 0 {
		return ""
	}
	var b strings.Builder
	var warn, err int
	for _, u := range j.Unmet {
		b.WriteString(u.line(name) + "\n")
		if u.Severity == config.Warning {
			warn++
		} else {
			err++
		}
	}
	fmt.Fprintf(&b, "  requirements: %d evaluated · %d met · %d unmet (warning) · %d unmet (error)\n", j.Evaluated, j.Evaluated-warn-err, warn, err)
	return b.String()
}
