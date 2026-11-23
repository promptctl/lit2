package root

import (
	"io"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/evaluate"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/reason"
	"github.com/promptctl/lit2/internal/verbs"
)

// get is get's body: the paths read against the folded state.
func (v inForce) get(ps []cli.PathText) cli.Outcome {
	got := verbs.Get(v.world(v.folded.State), texts(ps))
	v.rec.Told(v.facts(verbs.Compiled{Operands: got.Operands}, false, nil))
	if no, refused := got.Refusal(); refused {
		return cli.Failed{Failure: refusal{no}}
	}
	return written(io.WriteString(v.p.Out, got.Out))
}

// write is a write verb's body, the Evaluate and Perform steps
// (specifications/code-layout.md, "One command, start to finish"): compile every operand,
// mint the drafts and judge them against the state they fold onto, and
// unless one is refused, there is nothing to write or the run is dry,
// append them as one frame; then echo each change from that state, so a
// write lands whole or not at all.
func (v inForce) write(verb verbs.Verb, ops []verbs.Operand, flags cli.Flags) cli.Outcome {
	c := verbs.Compile(v.world(v.folded.State), verb, ops, func() (string, error) {
		b, err := io.ReadAll(v.p.In)
		return string(b), err
	})
	if no, refused := c.Refusal(); refused {
		v.rec.Told(v.facts(c, flags.DryRun, nil))
		return cli.Failed{Failure: refusal{no}}
	}
	if len(c.Drafts) == 0 {
		v.rec.Told(v.facts(c, flags.DryRun, nil))
		if flags.DryRun {
			return written(io.WriteString(v.p.Out, verbs.Echo(v.world(v.folded.State), c, nil)))
		}
		return cli.Done{}
	}
	m, err := v.mint(c.Drafts, v.ref)
	if err != nil {
		v.rec.Told(v.facts(c, flags.DryRun, nil))
		return failed(err)
	}
	after, c, outcomes := verbs.Folded(v.p.Fold, v.world(v.folded.State), c, m.Events)
	v.rec.Policy.Verdicts = command.Verdicts(m.Events, outcomes)
	if no, refused := c.Refusal(); refused {
		v.rec.Told(v.facts(c, flags.DryRun, nil))
		return cli.Failed{Failure: refusal{no}}
	}
	if flags.DryRun {
		v.rec.Told(v.facts(c, true, nil))
		return written(io.WriteString(v.p.Out, verbs.Echo(after, c, nil)))
	}
	// the minted ids are told once the store holds them, so the block never
	// names an event the store does not
	v.rec.Told(v.facts(c, false, nil))
	if err := v.append(m); err != nil {
		return failed(err)
	}
	v.rec.Told(v.facts(c, false, m.Events))
	return written(io.WriteString(v.p.Out, verbs.Echo(after, c, m.Events)))
}

func (v inForce) world(st fold.State) verbs.World {
	return verbs.World{View: evaluate.Of(st, v.schema, v.now), File: v.file, Me: v.id.Key.Principal()}
}

// facts is the verbs block of what a body did.
func (v inForce) facts(c verbs.Compiled, dry bool, minted []events.Event) command.VerbsFacts {
	f := command.VerbsFacts{Drafts: len(c.Drafts), Kinds: map[string]int{}, DryRun: dry, Requirements: command.RequirementCounts(c.Tally)}
	for _, o := range c.Operands {
		switch o := o.(type) {
		case verbs.Accepted:
			f.Operands = append(f.Operands, command.OperandFacts{Text: o.Text, Canon: o.Canon, Values: o.Values})
		case verbs.Refused:
			f.Operands = append(f.Operands, command.OperandFacts{Text: o.Text, Reason: o.Reason, Detail: o.Detail})
		}
	}
	count := func(p events.Payload) {
		f.Kinds[string(p.Kind())]++
		if verbs.Deletes(p) {
			f.Deletes++
		}
	}
	for _, d := range c.Drafts {
		switch d := d.(type) {
		case events.Act:
			count(d.Payload)
		case events.CreateElement:
			count(d.Payload)
		case events.CreateItem:
			count(d.Payload)
			for _, p := range d.Then {
				count(p)
			}
		}
	}
	for _, e := range minted {
		f.Minted = append(f.Minted, e.ID().String())
	}
	f.Rows.Builtin, f.Rows.Declared, f.Rows.Checks = v.schema.Table().Origins()
	return f
}

func texts(ps []cli.PathText) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = string(p)
	}
	return out
}

func pairs(ps []cli.Pair) []verbs.Operand {
	out := make([]verbs.Operand, len(ps))
	for i, p := range ps {
		out[i] = verbs.Operand{Path: string(p.Path), Value: string(p.Value)}
	}
	return out
}

func paths(ps []cli.PathText) []verbs.Operand {
	out := make([]verbs.Operand, len(ps))
	for i, p := range ps {
		out[i] = verbs.Operand{Path: string(p)}
	}
	return out
}

// refusal is an operand a verb refused: the command's, class refused.
type refusal struct{ r verbs.Refused }

func (f refusal) Error() string     { return f.r.Err.Error() }
func (refusal) Class() reason.Class { return reason.ClassRefused }
func (f refusal) Reason() string    { return f.r.Reason }
