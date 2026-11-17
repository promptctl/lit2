package verbs_test

import (
	"bytes"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/fold/foldtest"
	"github.com/promptctl/lit2/internal/verbs"
)

var joiner = must(events.NewKey(bytes.NewReader(bytes.Repeat([]byte{2}, 32))))

// TestBootstrapIsInitsStepOverTheLocalFold: a store with no enrollment is
// founded; over one, a member is listed, a stranger asks, and a stranger
// that asked is told so.
func TestBootstrapIsInitsStepOverTheLocalFold(t *testing.T) {
	if _, ok := verbs.Bootstrap(fold.State{}, key.Principal()).(verbs.Found); !ok {
		t.Errorf("over no enrollment: want Found")
	}
	if l, ok := verbs.Bootstrap(founded, key.Principal()).(verbs.Listed); !ok || l.Role != verbs.Owner {
		t.Errorf("the founder: %+v; want Listed as owner", l)
	}
	if _, ok := verbs.Bootstrap(founded, joiner.Principal()).(verbs.Ask); !ok {
		t.Errorf("a stranger: want Ask")
	}
	m := must(events.Mint(events.Context{Writer: two, Agent: events.Unlabeled{}, ConfigRef: ref, Policy: founded.Cites(),
		Frontier: founded.Frontier(), Known: founded.Known(), Now: time.Date(2026, time.November, 17, 9, 0, 0, 0, time.UTC)},
		events.NewDrafts(events.Act{About: events.Root{}, Payload: events.Request{}}), joiner.Signer(), rand.NewChaCha8([32]byte{'j'})))
	asked := fold.Extend(env, founded, m.Events).State
	if _, ok := verbs.Bootstrap(asked, joiner.Principal()).(verbs.Asked); !ok {
		t.Errorf("a stranger that asked: want Asked")
	}
}

// TestASoloOwnersAddDraftsTheProposalAndItsApproval: the owner's approval
// counts toward every change, so both an add of a contributor and of an
// owner draft it beside the proposal; the incoming owner's own approval is
// theirs to give.
func TestASoloOwnersAddDraftsTheProposalAndItsApproval(t *testing.T) {
	for role, want := range map[string][]events.Kind{
		"contributor": {events.Propose{}.Kind(), events.Approve{}.Kind()},
		"owner":       {events.Propose{}.Kind(), events.Approve{}.Kind()},
	} {
		b := newBook(t, profile(""))
		b.st = foldtest.Founded(env, key.Signer(), events.Enroll{Owner: verbs.Owner, Roles: verbs.DefaultRoles()})
		c := verbs.Compile(b.world(b.st), verbs.Add, []verbs.Operand{{Path: "/members", Value: `{principal: "` + joiner.Principal().String() + `", role: ` + role + `}`}},
			func() (string, error) { return "", nil })
		if no, refused := c.Refusal(); refused {
			t.Fatalf("add a %s: refused %s: %v", role, no.Reason, no.Err)
		}
		var got []events.Kind
		for _, d := range c.Drafts {
			got = append(got, d.(events.Act).Payload.Kind())
		}
		if len(got) != len(want) || got[0] != want[0] || len(got) > 1 && got[1] != want[1] {
			t.Errorf("add a %s drafts %v; want %v", role, got, want)
		}
	}
}
