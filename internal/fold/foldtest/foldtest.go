// Package foldtest is what a test above the fold needs to fold events of
// its own: a store founded, so the policy rule admits what it mints.
package foldtest

import (
	"bytes"
	"fmt"
	"time"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// Founder is the checkout that enrolls an Enrolled store: no checkout a
// test writes as, so the enrollment is the founder's stream alone.
var Founder = store.CheckoutID{0xf0}

// Owner is the role an Enrolled store's founder holds.
var Owner = events.MustName("owner")

// Enrolled is the state of a store signer founded and nothing else:
// version 1 in force, signer's principal its one owner, so every path
// write it signs is granted under env's rules.
func Enrolled(env fold.Env, signer events.Signer) fold.State {
	return Founded(env, signer, events.Enroll{Owner: Owner})
}

// Founded is the state of a store signer founded by e and nothing else.
func Founded(env fold.Env, signer events.Signer, e events.Enroll) fold.State {
	ref, err := git.ParseObjectID(git.SHA1, "2e65efe2a145dda7ee51d1741299f848e5bf752e")
	if err != nil {
		panic(err)
	}
	m, err := events.Mint(events.Context{Writer: Founder, Agent: events.Unlabeled{}, ConfigRef: ref, Policy: 1,
		Now: time.Date(2026, time.November, 17, 8, 0, 0, 0, time.UTC)},
		events.NewDrafts(events.Act{About: events.Root{}, Payload: e}), signer, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		panic(err)
	}
	out := fold.Extend(env, fold.State{}, m.Events)
	if _, ok := out.Outcomes[0].(fold.Applied); !ok {
		panic(fmt.Sprintf("foldtest: the enrollment is %#v; want it applied", out.Outcomes[0]))
	}
	return out.State
}
