package verbs

import (
	"errors"
	"fmt"
	"strings"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

// Folded is a compiled command judged on what its minted events fold to
// over seen, the world it compiled against, and that world: first the
// policy's verdict on each event, the one every replica reaches when it
// folds them `[LAW:single-enforcer]`, then the gate's on each arrival. A
// command the policy refuses any event of writes nothing, so its refusal
// names every event refused. The fold's outcome on each event is
// returned beside them, for the run to record.
func Folded(e fold.Env, seen World, c Compiled, minted []events.Event) (World, Compiled, []fold.Outcome) {
	ext := fold.Extend(e, seen.State(), minted)
	after := World{View: seen.Of(ext.State), File: seen.File}
	no, carried := Carried(seen, ext.Outcomes)
	if !carried {
		return after, Gate(after, c, minted), ext.Outcomes
	}
	operands := append([]Result(nil), c.Operands...)
	operands[0] = Refused{Text: operands[0].(Accepted).Text, Reason: no.Reason, Err: no.Err}
	c.Operands, c.Drafts, c.Changes = operands, nil, nil
	return after, c, ext.Outcomes
}

// Carried is the refusal of a command whose events the fold carried any
// of, read in w, the world they fold onto: the first event's reason, a
// line for each, and false when it carried none.
func Carried(w World, outcomes []fold.Outcome) (Refused, bool) {
	var errs []error
	var no Refused
	for _, o := range outcomes {
		switch o := o.(type) {
		case fold.Applied:
		case fold.CarriedFor:
			errs = append(errs, w.carried(o.Why))
			if no.Reason == "" {
				no.Reason = fold.ReasonName(o.Why)
			}
		case fold.LeftFor:
			// a kind this release mints and cannot read is the registry's
			// bug, which the fold's tests own; the event is written as before
		}
	}
	no.Err = errors.Join(errs...)
	return no, len(errs) > 0
}

// carried is the line of an event the fold carried for why: for a write
// the policy refused, the write, the writer's standing and each capability
// of its verb with what it missed, read from the world before the write.
func (w World) carried(why fold.Reason) error {
	u, ok := why.(fold.Unauthorized)
	if !ok {
		return fmt.Errorf("refused: %s; nothing written", fold.ReasonName(why))
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
	return errors.New(strings.Join(lines, "\n"))
}

// pathOf is a write's path as its operand was typed: an item by its id, a
// home by its name, the root as /.
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

// levelOf is the level subject sits at, as a level condition reads it.
func (w World) levelOf(at fold.Subject) string {
	it, ok := at.(fold.OfItem)
	if !ok {
		return "a home has no level"
	}
	if f, ok := w.State().Item(it.ID).(fold.Found); ok {
		if l, ok := f.Item.Level.(fold.Leveled); ok {
			return fmt.Sprintf("it is level %d", l.Level.Int())
		}
	}
	return "it has no level the fold knows"
}
