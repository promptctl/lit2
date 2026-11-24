package fold_test

import (
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/store"
)

var (
	keyC = keyOf(3)
	c    = writer{checkout: store.CheckoutID{0xc}, signer: keyC.Signer()}
)

// soloWorld is a store A founded and admitted B into as a contributor, in
// one command: version 2, A its one owner.
func soloWorld(t *testing.T) *world {
	t.Helper()
	w := unfounded(t)
	w.found(founder, fold.State{}, root(events.Enroll{Owner: owner, Roles: roles}))
	admit := propose(w.state(), map[events.PrincipalID]events.Name{keyA.Principal(): owner, keyB.Principal(): contributor}, events.Majority{})
	w.mint(founder, w.state(), root(admit), root(events.Approve{Proposal: admit.Digest()}))
	w.base = w.state()
	if v := version(t, w.base); v != 2 {
		t.Fatalf("a solo owner's admission left version %d in force, want 2", v)
	}
	return w
}

// propose is a proposal over the version in force at s.
func propose(s fold.State, members map[events.PrincipalID]events.Name, th events.Threshold) events.Propose {
	return events.Propose{Base: s.Policy().(fold.Enrolled).Policy.Digest(), Members: members, Threshold: th}
}

func version(t *testing.T, s fold.State) events.PolicyVersion {
	t.Helper()
	p, ok := s.Policy().(fold.Enrolled)
	if !ok {
		t.Fatalf("no policy is in force: %#v", s.Policy())
	}
	return p.Policy.Version()
}

// carried is why s carried e, and false when it did not.
func carried(s fold.State, e events.Event) (fold.Reason, bool) {
	for _, c := range s.Integrity().Carried {
		if c.Event == e.ID() {
			return c.Reason, true
		}
	}
	return nil, false
}

func wantCarried(t *testing.T, s fold.State, e events.Event, want fold.Reason) {
	t.Helper()
	got, ok := carried(s, e)
	if !ok || !fold.ReasonEqual(got, want) {
		t.Errorf("%s is carried %v for %#v; want %#v", e.Header().Kind(), ok, got, want)
	}
}

func wantApplied(t *testing.T, s fold.State, e events.Event) {
	t.Helper()
	if why, ok := carried(s, e); ok {
		t.Errorf("%s is carried for %#v; want it applied", e.Header().Kind(), why)
	}
}

func TestAnEventBeforeTheEnrollmentIsCarriedAndOneAfterItApplies(t *testing.T) {
	w := unfounded(t)
	early := w.mint(a, fold.State{}, create(events.AtLevel{Level: level(1)}))[0]
	w.found(founder, w.state(), root(events.Enroll{Owner: owner, Roles: roles}))
	after := w.mint(a2, w.state(), create(events.AtLevel{Level: level(1)}))[0]
	s := w.state()
	wantCarried(t, s, early, fold.NoPolicyInForce{})
	wantApplied(t, s, after)
	if _, ok := s.Item(idOf(early)).(fold.NotFound); !ok {
		t.Error("an item created before the enrollment was folded")
	}
	if v := version(t, s); v != 1 {
		t.Errorf("the enrollment founds version %d, want 1", v)
	}
}

func TestAnEnrollmentNotTheFoundingFoundsNothingAndItsWriterIsNoMember(t *testing.T) {
	w := unfounded(t)
	w.found(founder, fold.State{}, root(events.Enroll{Owner: owner, Roles: roles}))
	second := w.mint(b, w.state(), root(events.Enroll{Owner: owner, Roles: roles}))[0]
	filed := w.mint(b, w.state(), create(events.AtLevel{Level: level(1)}))[0]
	s := w.state()
	wantCarried(t, s, second, fold.NotTheFounding{})
	wantCarried(t, s, filed, fold.Unauthorized{Intent: fold.PathWrite{Verb: events.Add, At: fold.OfHome{Holds: level(1)}, Field: []string{"children"}},
		Refused: fold.Refused{As: fold.NotAMember{}}})
	if owners := s.Policy().(fold.Enrolled).Policy.Owners(); len(owners) != 1 || owners[0] != keyA.Principal() {
		t.Errorf("the owners are %v, want A alone", owners)
	}
}

func TestASoloOwnerAdmitsAMemberWhoseEventsApplyUnderTheNewVersion(t *testing.T) {
	w := soloWorld(t)
	filed := w.mint(b, w.base, create(events.AtLevel{Level: level(1)}))[0]
	s := w.state()
	wantApplied(t, s, filed)
	if role, ok := s.Policy().(fold.Enrolled).Policy.Role(keyB.Principal()); !ok || role != contributor {
		t.Errorf("B's role is %s %v, want contributor", role, ok)
	}
	out := fold.Extend(env, w.base, []events.Event{filed})
	if ap, ok := out.Outcomes[0].(fold.Applied); !ok || ap.Under != 2 {
		t.Errorf("B's create is %#v, want applied under version 2", out.Outcomes[0])
	}
}

func TestAContributorsRmOfAnotherOwnersEpicIsRefusedNamingWhatItNeeded(t *testing.T) {
	w := soloWorld(t)
	epic := idOf(w.mint(a, w.base, create(events.AtLevel{Level: level(2)}))[0])
	leaf := idOf(w.mint(a, w.state(), create(events.AtLevel{Level: level(1)}))[0])
	rm := w.mint(b, w.state(), set(epic, fold.Deleted, plain("true")))[0]
	moved := w.mint(b, w.state(), events.Act{About: events.Item{ID: leaf}, Payload: events.Transition{From: name("backlog"), To: name("done")}})[0]
	s := w.state()
	wantCarried(t, s, rm, fold.Unauthorized{Intent: fold.PathWrite{Verb: events.Rm, At: fold.OfItem{ID: epic}},
		Refused: fold.Refused{As: fold.AsRole{Role: contributor}, Misses: []fold.Miss{
			{Capability: events.MustCapability("rm own/*/*"), Failed: []fold.Failure{fold.PathOutside{}, fold.BindingUnmet{}}},
			{Capability: events.MustCapability("rm own/* (level=1)"),
				Failed: []fold.Failure{fold.BindingUnmet{}, fold.ConditionUnmet{Condition: events.LevelIs{Op: events.Eq, Level: level(1)}}}}}}})
	if _, deleted := found(t, s, epic).Facts.Plain(fold.Deleted).(fold.Settled); deleted {
		t.Error("the refused rm deleted the epic")
	}
	wantApplied(t, s, moved)
}

func TestAContributorRemovesItsOwnTicketAndLabelsIt(t *testing.T) {
	w := soloWorld(t)
	mine := idOf(w.mint(b, w.base, create(events.AtLevel{Level: level(1)}))[0])
	label := w.mint(b, w.state(), events.Act{About: events.Item{ID: mine}, Payload: events.MemberAdd{Field: labels, Member: name("urgent")}})[0]
	rm := w.mint(b, w.state(), set(mine, fold.Deleted, plain("true")))[0]
	s := w.state()
	wantApplied(t, s, label)
	wantApplied(t, s, rm)
}

func TestAddingAnOwnerNeedsTheOtherOwnerAndTheIncomingOne(t *testing.T) {
	// the incoming owner's approval alone is held: the quorum is A and B
	without := newWorld(t)
	held := propose(without.base, map[events.PrincipalID]events.Name{keyA.Principal(): owner, keyB.Principal(): owner, keyC.Principal(): owner}, events.Majority{})
	without.mint(a, without.base, root(held), root(events.Approve{Proposal: held.Digest()}))
	without.mint(b, without.state(), root(events.Approve{Proposal: held.Digest()}))
	if v := version(t, without.state()); v != 2 {
		t.Fatalf("an owner was added without its own approval: version %d is in force", v)
	}
	w := newWorld(t)
	three := propose(w.base, map[events.PrincipalID]events.Name{keyA.Principal(): owner, keyB.Principal(): owner, keyC.Principal(): owner}, events.Majority{})
	w.mint(a, w.base, root(three), root(events.Approve{Proposal: three.Digest()}))
	if v := version(t, w.state()); v != 2 {
		t.Fatalf("one owner of two added an owner: version %d is in force", v)
	}
	w.mint(c, w.state(), root(events.Approve{Proposal: three.Digest()}))
	if v := version(t, w.state()); v != 2 {
		t.Fatalf("an owner was added without the other owner's approval: version %d is in force", v)
	}
	w.mint(b, w.state(), root(events.Approve{Proposal: three.Digest()}))
	s := w.state()
	if v := version(t, s); v != 3 || len(s.Policy().(fold.Enrolled).Policy.Owners()) != 3 {
		t.Errorf("version %d with owners %v is in force, want 3 with three owners", v, s.Policy().(fold.Enrolled).Policy.Owners())
	}
	if ps := s.Proposals(); len(ps) != 0 {
		t.Errorf("an activated proposal is still listed: %#v", ps)
	}
}

func TestRaisingTheThresholdNeedsBothOwners(t *testing.T) {
	w := newWorld(t)
	both := propose(w.base, w.base.Policy().(fold.Enrolled).Policy.MemberMap(), events.Fixed{N: 2})
	w.mint(a, w.base, root(both), root(events.Approve{Proposal: both.Digest()}))
	if ps := w.state().Proposals(); len(ps) != 1 || version(t, w.state()) != 2 {
		t.Fatalf("one owner of two raised the threshold: %#v", ps)
	}
	w.mint(b, w.state(), root(events.Approve{Proposal: both.Digest()}))
	if v := version(t, w.state()); v != 3 {
		t.Errorf("both owners' approvals left version %d in force, want 3", v)
	}
}

func TestAnOwnerRemovesAnotherAloneAndTheRemovedOnesLaterEventsAreCarried(t *testing.T) {
	w := newWorld(t)
	before := w.mint(b, w.base, create(events.AtLevel{Level: level(1)}))[0]
	alone := propose(w.state(), map[events.PrincipalID]events.Name{keyA.Principal(): owner}, events.Majority{})
	w.mint(a, w.state(), root(alone), root(events.Approve{Proposal: alone.Digest()}))
	after := w.mint(b, w.state(), create(events.AtLevel{Level: level(1)}))[0]
	s := w.state()
	if v := version(t, s); v != 3 {
		t.Fatalf("removing B left version %d in force, want 3", v)
	}
	wantApplied(t, s, before)
	wantCarried(t, s, after, fold.Unauthorized{Intent: fold.PathWrite{Verb: events.Add, At: fold.OfHome{Holds: level(1)}, Field: []string{"children"}},
		Refused: fold.Refused{As: fold.NotAMember{}}})
}

func TestEveryPolicyActThePolicyRuleRefusesIsCarriedWithItsWriter(t *testing.T) {
	w := soloWorld(t)
	raise := propose(w.base, w.base.Policy().(fold.Enrolled).Policy.MemberMap(), events.Fixed{N: 1})
	w.mint(a, w.base, root(raise))
	notApprover := w.mint(b, w.state(), root(events.Approve{Proposal: raise.Digest()}))[0]
	unknown := w.mint(a, w.state(), root(events.Approve{Proposal: events.Digest{9}}))[0]
	ownerless := w.mint(a, w.state(), root(propose(w.state(), map[events.PrincipalID]events.Name{keyB.Principal(): contributor}, events.Majority{})))[0]
	byContributor := propose(w.state(), map[events.PrincipalID]events.Name{keyA.Principal(): owner, keyB.Principal(): owner}, events.Majority{})
	grab := w.mint(b, w.state(), root(byContributor))[0]
	s := w.state()
	wantCarried(t, s, notApprover, fold.NotAnApprover{Proposal: raise.Digest()})
	wantCarried(t, s, unknown, fold.UnknownProposal{Proposal: events.Digest{9}})
	if why, ok := carried(s, ownerless); !ok {
		t.Error("an ownerless proposal applied")
	} else if _, inadmissible := why.(fold.Inadmissible); !inadmissible {
		t.Errorf("an ownerless proposal is carried for %#v, want Inadmissible", why)
	}
	if why, ok := carried(s, grab); !ok {
		t.Error("a contributor's proposal applied")
	} else if u, unauthorized := why.(fold.Unauthorized); !unauthorized || u.Intent.String() != "set /members/"+keyB.Principal().String()+"/role" {
		t.Errorf("a contributor's proposal is carried for %#v, want its role move unauthorized", why)
	}
	for _, c := range s.Integrity().Carried {
		if c.Event == notApprover.ID() && (c.Writer != b.checkout || c.Principal != keyB.Principal()) {
			t.Errorf("the carried approval names writer %v, principal %s", c.Writer, c.Principal)
		}
	}
}

// mintCiting is mint with the header citing cited.
func (wd *world) mintCiting(w writer, seen fold.State, cited events.PolicyVersion, d events.Draft) events.Event {
	wd.t.Helper()
	m, err := events.Mint(events.Context{Writer: w.checkout, Agent: events.Unlabeled{}, ConfigRef: configRef, Policy: cited,
		Frontier: seen.Frontier(), Known: seen.Known(), Now: t0}, events.NewDrafts(d), w.signer, wd.entropy)
	if err != nil {
		wd.t.Fatal(err)
	}
	wd.append(w.ref(), m.Batch)
	return m.Events[0]
}

func TestAWriterCitesNeitherAFutureVersionNorOneOlderThanItCited(t *testing.T) {
	w := newWorld(t)
	future := w.mintCiting(a, w.base, 9, create(events.AtLevel{Level: level(1)}))
	w.mint(b, w.state(), create(events.AtLevel{Level: level(1)}))
	older := w.mintCiting(b, w.state(), 1, create(events.AtLevel{Level: level(1)}))
	s := w.state()
	wantCarried(t, s, future, fold.CitesFutureVersion{Cited: 9, InForce: 2})
	wantCarried(t, s, older, fold.VersionRegressed{Cited: 1, Previous: 2})
}

func TestOneEventSetFoldsEqualInAnyStreamOrder(t *testing.T) {
	w := soloWorld(t)
	epic := idOf(w.mint(a, w.base, create(events.AtLevel{Level: level(2)}))[0])
	w.mint(b, w.state(), set(epic, fold.Deleted, plain("true")))
	w.mint(writer{checkout: checkoutB, signer: forger{keyB.Signer()}}, w.state(), create(events.AtLevel{Level: level(1)}))
	w.mint(c, w.base, root(events.Enroll{Owner: owner, Roles: roles}))
	w.mint(c, w.state(), root(events.Request{}))
	alone := propose(w.state(), map[events.PrincipalID]events.Name{keyA.Principal(): owner}, events.Majority{})
	w.mint(a, w.state(), root(alone), root(events.Approve{Proposal: alone.Digest()}))
	w.mint(b, w.state(), create(events.AtLevel{Level: level(1)}))
	want := w.state()
	if n := len(want.Integrity().Carried); n < 4 {
		t.Fatalf("the set carries %d events, want the rm, the forgery, the enrollment and the removed member's create", n)
	}
	scans := w.scans()
	for _, order := range [][]int{{0, 1, 2, 3, 4, 5}, {5, 4, 3, 2, 1, 0}, {2, 5, 0, 4, 1, 3}} {
		var in []store.Scanned
		for _, i := range order {
			if i < len(scans) {
				in = append(in, scans[i])
			}
		}
		in = append(in, scans[0]) // a copy of the founder's stream, read twice
		got := fold.Replay(env, w.founding, in).State
		if !got.Equal(want) {
			t.Errorf("the streams in order %v fold to another state", order)
		}
		if len(got.Requests()) != 1 || !slices.EqualFunc(got.Integrity().Carried, want.Integrity().Carried, func(x, y fold.Carried) bool {
			return x.Event == y.Event && fold.ReasonEqual(x.Reason, y.Reason)
		}) {
			t.Errorf("the streams in order %v carry other events or requests", order)
		}
	}
}
