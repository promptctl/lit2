package verbs

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

// Folded is a compiled command judged on what its minted events fold to
// over seen, the world it compiled against, and that world: first the
// policy's verdict on each event, the one every replica reaches when it
// folds them `[LAW:single-enforcer]`, then the gate's on each arrival. A
// command the policy refuses any event of writes nothing: each operand
// with an event refused is refused, the first naming every event refused.
// The fold's outcome on each event is returned beside them, for the run to
// record.
func Folded(e fold.Env, seen World, c Compiled, minted []events.Event) (World, Compiled, []fold.Outcome) {
	ext := fold.Extend(e, seen.State(), minted)
	after := seen
	after.View = seen.Of(ext.State)
	no, carried := Carried(seen, ext.Outcomes)
	if !carried {
		return after, Gate(after, c, minted), ext.Outcomes
	}
	byOperand := map[int][]fold.Outcome{}
	for i, op := range mintedBy(c) {
		byOperand[op] = append(byOperand[op], ext.Outcomes[i])
	}
	operands := slices.Clone(c.Operands)
	first := true
	for i, res := range operands {
		r, refused := Carried(seen, byOperand[i])
		if !refused {
			continue
		}
		if first {
			r.err, first = no.err, false
		}
		operands[i] = Refused{Text: res.(Accepted).Text, Reason: r.Reason, err: r.err}
	}
	c.Operands, c.Drafts, c.Changes = operands, nil, nil
	return after, c, ext.Outcomes
}

// mintedBy is the operand each event c mints is of, in mint order: an
// operand's drafts are consecutive, and a create's Then follow it. Only a
// command compile refused no operand of is minted, so each is Accepted.
func mintedBy(c Compiled) []int {
	var out []int
	d := 0
	for i, r := range c.Operands {
		for range r.(Accepted).Drafts {
			n := 1
			if ci, ok := c.Drafts[d].(events.CreateItem); ok {
				n += len(ci.Then)
			}
			for range n {
				out = append(out, i)
			}
			d++
		}
	}
	return out
}

// Carried is the refusal of a command whose events the fold carried any
// of, read in w, the world they fold onto: the first event's reason, a
// line for each, and false when it carried none.
func Carried(w World, outcomes []fold.Outcome) (Refused, bool) {
	var errs []error
	var why string
	for _, o := range outcomes {
		switch o := o.(type) {
		case fold.Applied:
		case fold.CarriedFor:
			errs = append(errs, w.carried(o.Why))
			if why == "" {
				why = fold.ReasonName(o.Why)
			}
		case fold.LeftFor:
			// a kind this release mints and cannot read is the registry's
			// bug, which the fold's tests own; the event is written as before
		}
	}
	if len(errs) == 0 {
		return Refused{}, false
	}
	return Refused{Reason: why, err: errors.Join(errs...)}, true
}

// CarriedEvent is an event the fold carried, and Why it did: the error of
// a write the fold refused, which says the write and why.
// [LAW:one-type-per-behavior] one type for every reason the fold carries
// an event, the reason its value.
type CarriedEvent struct {
	Why  fold.Reason
	line string
}

func (c CarriedEvent) Error() string { return c.line }

// carried is the line of an event the fold carried for why: for a write
// the policy refused, the write, the writer's standing and each capability
// of its verb with what it missed, read from the world before the write.
func (w World) carried(why fold.Reason) error {
	return CarriedEvent{Why: why, line: w.carriedLine(why)}
}

func (w World) carriedLine(why fold.Reason) string {
	u, ok := why.(fold.Unauthorized)
	if !ok {
		return fmt.Sprintf("refused: %s; nothing written", fold.ReasonName(why))
	}
	write := fmt.Sprintf("%s %s", u.Intent.Verb, w.pathOf(u.Intent))
	var lines []string
	switch as := u.Refused.As.(type) {
	case fold.NotAMember:
		lines = append(lines, write+" refused: you are not a member of this store; nothing written")
	case fold.AsRole:
		lines = append(lines, fmt.Sprintf("%s refused: role %s holds no capability that allows it; nothing written", write, as.Role))
	}
	for _, m := range u.Refused.Misses {
		var why []string
		for _, f := range m.Failed {
			why = append(why, w.failure(u.Intent.At, m.Capability, f))
		}
		lines = append(lines, fmt.Sprintf("  %s: %s", m.Capability, strings.Join(why, "; ")))
	}
	return strings.Join(lines, "\n")
}

// pathOf is a write's path as its operand was typed: an item by its id,
// one the command creates as (new), a comment by its item and its segment,
// a home by its name, the root as /.
func (w World) pathOf(p fold.PathWrite) string {
	var at string
	switch s := p.At.(type) {
	case fold.OfRoot:
		at = ""
	case fold.OfHome:
		l, _ := w.Schema().Level(s.Holds.Int()) // a home exists at every level the schema declares
		at = l.Home
	case fold.OfItem:
		at = w.render(s.ID)
		if _, made := w.State().Item(s.ID).(fold.NotFound); made {
			at = "(new)" // created by the command the write is of, as the echo names it
		}
	case fold.OfElement:
		at = w.render(s.Of) + "/comments/" + w.segmentOf(s)
	}
	return strings.Join(append([]string{at}, p.Field...), "/")
}

// failure is what a write at subject failed of capability c, as a fact of
// the world: the fold names the part, the world what the subject holds.
func (w World) failure(at fold.Subject, c events.Capability, f fold.Failure) string {
	switch f := f.(type) {
	case fold.PathOutside:
		return "the path is outside it"
	case fold.BindingUnmet:
		if c.Binding == events.Assignee {
			return "no item has an assignee yet"
		}
		return "you did not create it"
	case fold.ConditionUnmet:
		switch f.Condition.(type) {
		case events.LevelIs:
			return w.levelOf(at)
		}
	}
	panic(fmt.Sprintf("verbs: %T is not a Failure", f))
}

// segmentOf is the segment comment e's create fixed, which a path names it
// by; its id when nothing created it.
func (w World) segmentOf(e fold.OfElement) string {
	if f, ok := w.State().Item(e.Of).(fold.Found); ok {
		for _, el := range f.Item.Elements {
			if c, ok := fold.CreateOf(el.Origin); ok && el.ID == e.ID {
				return c.Segment.String()
			}
		}
	}
	return e.ID.String()
}

// levelOf is the level subject sits at, as a level condition reads it.
func (w World) levelOf(at fold.Subject) string {
	it, ok := at.(fold.OfItem)
	if !ok {
		return "only an item has a level"
	}
	if f, ok := w.State().Item(it.ID).(fold.Found); ok {
		if l, ok := f.Item.Level.(fold.Leveled); ok {
			return fmt.Sprintf("it is level %d", l.Level.Int())
		}
	}
	return "it has no level the fold knows"
}
