package fold_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

// judged is the outcome of folding e over s.
func judged(s fold.State, e events.Event) fold.Outcome {
	return fold.Extend(env, s, []events.Event{e}).Outcomes[0]
}

func TestAContributorIsGrantedByTheCapabilityThatMatches(t *testing.T) {
	w := soloWorld(t)
	theirs := idOf(w.mint(a, w.base, create(events.AtLevel{Level: level(1)}))[0])
	for _, c := range []struct {
		name  string
		draft events.Draft
		by    string
	}{
		{"a create at a level's home", create(events.AtLevel{Level: level(1)}), "add */children"},
		{"a child under another's item", createUnder(theirs), "add */children"},
		{"a move of another's item", events.Act{About: events.Item{ID: theirs}, Payload: events.Transition{From: name("backlog"), To: name("done")}}, "set */status"},
		{"a comment on another's item", comment(theirs, "c"), "add */comments"},
	} {
		t.Run(c.name, func(t *testing.T) {
			seen := w.state()
			e := w.mint(b, seen, c.draft)[0]
			ap, ok := judged(seen, e).(fold.Applied)
			if !ok {
				t.Fatalf("it is %#v, want applied", judged(seen, e))
			}
			if g, ok := ap.Grant.(fold.ByCapability); !ok || g.Capability.String() != c.by {
				t.Errorf("it is granted by %#v, want %s", ap.Grant, c.by)
			}
		})
	}
}

func TestAContributorsRefusalNamesEveryCapabilityOfTheVerbAndWhatItMissed(t *testing.T) {
	w := soloWorld(t)
	theirs := idOf(w.mint(a, w.base, create(events.AtLevel{Level: level(1)}))[0])
	epic := idOf(w.mint(b, w.state(), create(events.AtLevel{Level: level(2)}))[0])
	rmOwn := events.MustCapability("rm own/* (level=1)")
	rmComment := events.MustCapability("rm own/comments/*")
	for _, c := range []struct {
		name   string
		draft  events.Draft
		misses []fold.Miss
	}{
		{"an rm of another's ticket", set(theirs, fold.Deleted, plain("true")), []fold.Miss{
			{Capability: rmComment, Failed: []fold.Failure{fold.PathOutside{}, fold.BindingUnmet{}}},
			{Capability: rmOwn, Failed: []fold.Failure{fold.BindingUnmet{}}}}},
		{"an rm of its own epic", set(epic, fold.Deleted, plain("true")), []fold.Miss{
			{Capability: rmComment, Failed: []fold.Failure{fold.PathOutside{}}},
			{Capability: rmOwn, Failed: []fold.Failure{fold.ConditionUnmet{Condition: events.LevelIs{Op: events.Eq, Level: level(1)}}}}}},
		{"a title on another's ticket", set(theirs, title, prose("t")), []fold.Miss{
			{Capability: events.MustCapability("set own/*"), Failed: []fold.Failure{fold.BindingUnmet{}}},
			{Capability: events.MustCapability("set */status"), Failed: []fold.Failure{fold.PathOutside{}}}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			seen := w.state()
			e := w.mint(b, seen, c.draft)[0]
			cf, ok := judged(seen, e).(fold.CarriedFor)
			if !ok {
				t.Fatalf("it is %#v, want carried", judged(seen, e))
			}
			u, ok := cf.Why.(fold.Unauthorized)
			want := fold.Refused{As: fold.AsRole{Role: contributor}, Misses: c.misses}
			if !ok || !fold.ReasonEqual(u, fold.Unauthorized{Intent: u.Intent, Refused: want}) {
				t.Errorf("it is carried for %#v, want %#v", cf.Why, want)
			}
		})
	}
}

func TestAnAssigneeBindingMatchesNoItem(t *testing.T) {
	w := unfounded(t)
	helper := name("helper")
	w.mint(founder, fold.State{}, root(events.Enroll{Owner: owner, Roles: map[events.Name][]events.Capability{helper: capabilities("set assignee/*", "set own-or-assignee/*")}}))
	admit := propose(w.state(), map[events.PrincipalID]events.Name{keyA.Principal(): owner, keyB.Principal(): helper}, events.Majority{})
	w.mint(founder, w.state(), root(admit), root(events.Approve{Proposal: admit.Digest()}))
	theirs := idOf(w.mint(a, w.state(), create(events.AtLevel{Level: level(1)}))[0])
	seen := w.state()
	e := w.mint(b, seen, set(theirs, title, prose("t")))[0]
	cf, ok := judged(seen, e).(fold.CarriedFor)
	if !ok {
		t.Fatalf("a helper's title is %#v, want carried", judged(seen, e))
	}
	if u, ok := cf.Why.(fold.Unauthorized); !ok || len(u.Refused.Misses) != 2 {
		t.Errorf("a helper's title is carried for %#v, want both bindings unmet", cf.Why)
	}
}
