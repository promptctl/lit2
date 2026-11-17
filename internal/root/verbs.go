package root

import (
	"fmt"
	"io"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/evaluate"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/reason"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/verbs"
)

// get is get's body: the paths read against the folded state.
func (r body) get(f fold.Folded, ps []cli.PathText) cli.Outcome {
	got := verbs.Get(r.world(f.State), texts(ps))
	r.told(r.facts(got.Operands, nil, false, nil))
	if no, refused := got.Refusal(); refused {
		return cli.Failed{Failure: refusal{no}}
	}
	return written(io.WriteString(r.p.Out, got.Out))
}

// write is a write verb's body, §5 steps 5 and 6: compile every operand,
// mint the drafts and judge them against the state they fold onto, and
// unless one is refused, there is nothing to write or the run is dry,
// append them as one frame; then echo each change from that state, so a
// write lands whole or not at all.
func (r body) write(f fold.Folded, verb verbs.Verb, ops []verbs.Operand, flags cli.Flags) cli.Outcome {
	c := verbs.Compile(r.world(f.State), verb, ops, func() (string, error) {
		b, err := io.ReadAll(r.p.In)
		return string(b), err
	})
	if no, refused := c.Refusal(); refused {
		r.told(r.facts(c.Operands, c.Drafts, flags.DryRun, nil))
		return cli.Failed{Failure: refusal{no}}
	}
	if len(c.Drafts) == 0 {
		r.told(r.facts(c.Operands, c.Drafts, flags.DryRun, nil))
		if flags.DryRun {
			return written(io.WriteString(r.p.Out, verbs.Echo(r.world(f.State), c, nil)))
		}
		return cli.Done{}
	}
	opened := r.p.Store.(command.Opened)
	id := r.p.Place.(command.InCheckout).Identity.(command.Minted) // a store opens only for a minted checkout
	ctx := events.Context{Writer: id.Checkout, Agent: r.p.Agent, ConfigRef: r.loaded.Ref(opened.Format), Policy: 1,
		Frontier: f.State.Frontier(), Known: f.State.Known(), Now: r.now}
	m, err := events.Mint(ctx, events.NewDrafts(c.Drafts[0], c.Drafts[1:]...), id.Key.Signer(), r.p.Entropy)
	if err != nil {
		r.told(r.facts(c.Operands, c.Drafts, flags.DryRun, nil))
		return cli.Failed{Failure: command.Internal{Err: fmt.Errorf("minting: %w", err)}}
	}
	st := f.State
	for _, checked := range fold.Check(r.p.Fold, m.Events) {
		st = fold.Step(r.p.Fold.Registry(), st, checked)
	}
	after := r.world(st)
	c = verbs.Gate(after, c, m.Events)
	if no, refused := c.Refusal(); refused {
		r.told(r.facts(c.Operands, c.Drafts, flags.DryRun, nil))
		return cli.Failed{Failure: refusal{no}}
	}
	if flags.DryRun {
		r.told(r.facts(c.Operands, c.Drafts, true, nil))
		return written(io.WriteString(r.p.Out, verbs.Echo(after, c, nil)))
	}
	// the minted ids are told once the store holds them, so the block never
	// names an event the store does not
	r.told(r.facts(c.Operands, c.Drafts, false, nil))
	appended, err := opened.Store.Append(m.Batch)
	if err != nil {
		return cli.Failed{Failure: storeFailure{err: err}}
	}
	r.stored.Appends = append(r.stored.Appends, appended)
	r.told(r.facts(c.Operands, c.Drafts, false, m.Events))
	return written(io.WriteString(r.p.Out, verbs.Echo(after, c, m.Events)))
}

// init is admin init: the checkout's id and the principal's key, each
// minted once, and the principal printed, with the recovery phrase when
// this run made the key.
func (r body) init() cli.Outcome {
	in, ok := r.p.Place.(command.InCheckout)
	if !ok {
		return cli.Failed{Failure: storeFailure{err: fmt.Errorf("admin init makes a git checkout a writer, and this is none")}}
	}
	shared, private := r.p.IdentityFiles(in.Checkout)
	minted, fresh, err := MintIdentity(shared, private, r.p.Entropy)
	if err != nil {
		return cli.Failed{Failure: storeFailure{err: err}}
	}
	r.stored.Inits = append(r.stored.Inits, fresh)
	out := fmt.Sprintf("principal %s\n", minted.Key.Principal())
	if fresh.Key == store.Created {
		out += fmt.Sprintf("recovery phrase: %s\nwrite it down: it is the one way to restore this key\n", minted.Key.Phrase())
	}
	return written(io.WriteString(r.p.Out, out))
}

func (r body) world(st fold.State) verbs.World {
	return verbs.World{View: evaluate.Of(st, r.loaded.Schema(), r.now)}
}

// facts is the verbs block of what a body did.
func (r body) facts(ops []verbs.Result, drafts []events.Draft, dry bool, minted []events.Event) command.VerbsFacts {
	f := command.VerbsFacts{Drafts: len(drafts), Kinds: map[string]int{}, DryRun: dry}
	for _, o := range ops {
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
	for _, d := range drafts {
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
	f.Rows.Builtin, f.Rows.Declared, f.Rows.Checks = r.loaded.Schema().Table().Origins()
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
