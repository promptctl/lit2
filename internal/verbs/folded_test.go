package verbs_test

import (
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/fold/foldtest"
	"github.com/promptctl/lit2/internal/verbs"
)

// TestARefusalIsTheOperandsWhoseWriteWasRefused: a contributor setting
// its own story's title and the founder's in one command writes neither,
// and the refusal is the second operand's.
func TestARefusalIsTheOperandsWhoseWriteWasRefused(t *testing.T) {
	b := newBook(t, profile(""))
	b.st = foldtest.Founded(env, key.Signer(), events.Enroll{Owner: verbs.Owner, Roles: verbs.DefaultRoles()})
	b.do(verbs.Add, op("/members", `{principal: "`+joiner.Principal().String()+`", role: contributor}`))
	theirs := filed(t, b.do(verbs.Add, op("unfiled/children", "{title: a}")))
	asJoiner := func(verb verbs.Verb, ops ...verbs.Operand) (verbs.World, verbs.Compiled, []events.Event) {
		t.Helper()
		w := b.world(b.st)
		w.Me = joiner.Principal()
		c := verbs.Compile(w, verb, ops, func() (string, error) { return "", nil })
		if no, refused := c.Refusal(); refused {
			t.Fatalf("%s %+v refused at compile: %s: %v", verb, ops, no.Reason, no.Err())
		}
		m := must(events.Mint(events.Context{Writer: two, Agent: events.Unlabeled{}, ConfigRef: ref, Policy: b.st.Cites(),
			Frontier: b.st.Frontier(), Known: b.st.Known(), Now: time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC)},
			events.NewDrafts(c.Drafts[0], c.Drafts[1:]...), joiner.Signer(), b.entropy))
		return w, c, m.Events
	}
	_, _, minted := asJoiner(verbs.Add, op("unfiled/children", "{title: b}"))
	b.st = fold.Extend(env, b.st, minted).State
	id := minted[0].Header().About().(events.NewItem).ID
	mine := events.Render(b.schema.Prefix(), id, b.st.Path(id))

	w, c, minted := asJoiner(verbs.Set, op(mine+"/title", "x"), op(theirs+"/title", "y"))
	_, c, _ = verbs.Folded(env, w, c, minted)
	if _, ok := c.Operands[0].(verbs.Accepted); !ok {
		t.Errorf("the contributor's own title is %#v; want it accepted", c.Operands[0])
	}
	if r, ok := c.Operands[1].(verbs.Refused); !ok || r.Reason != "unauthorized" {
		t.Errorf("the founder's title is %#v; want it refused unauthorized", c.Operands[1])
	}
	if len(c.Drafts) != 0 {
		t.Errorf("a refused command drafts %d events", len(c.Drafts))
	}
}
