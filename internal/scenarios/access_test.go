package scenarios

import (
	"errors"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/cli"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/root"
	"github.com/promptctl/lit2/internal/verbs"
)

// lit is one run of tw, its streams read and then emptied, and the event
// it recorded.
func lit(tw root.Twin, args ...string) (out, errOut string, status int, event command.Event) {
	status = root.Run(tw.Process, args)
	out, errOut = tw.Out.String(), tw.Err.String()
	tw.Out.Reset()
	tw.Err.Reset()
	return out, errOut, status, tw.Commands.Events[len(tw.Commands.Events)-1]
}

func carriedUnauthorized(o fold.Outcome) bool {
	c, ok := o.(fold.CarriedFor)
	return ok && isUnauthorized(c.Why)
}

func isUnauthorized(why fold.Reason) bool {
	_, unauthorized := why.(fold.Unauthorized)
	return unauthorized
}

func principal(tw root.Twin) string {
	return tw.Principal().String()
}

// TestScenario15AccessControlFromTheFirstEvent walks scenario 15's steps
// 1 to 3 over two checkouts of one store, an owner's and a joiner's. The
// tiers, the sync and the re-publishing are later work; every step here
// is the fold's verdict, the one every replica reaches.
func TestScenario15AccessControlFromTheFirstEvent(t *testing.T) {
	owner := root.NewFreshTwin()
	// 1. the enrollment is the store's first event, its founder the owner
	out, errOut, status, event := lit(owner, "admin", "init")
	if status != 0 || !strings.Contains(out, "enrolled: policy v1, owner "+principal(owner)) {
		t.Fatalf("the owner's admin init: status %d, out %q, stderr %q", status, out, errOut)
	}
	if p := event.Policy; p.Bootstrap != "enroll" || len(p.Verdicts) != 2 {
		t.Errorf("the init's policy block is %+v; want the enrollment and version one judged", p)
	}

	// 2. the joiner's init requests membership; the solo owner admits them
	// in one command, its proposal and its approval
	joiner := root.NewTwinBeside(owner)
	if out, errOut, status, event := lit(joiner, "admin", "init"); status != 0 || !strings.Contains(out, "membership requested") || event.Policy.Bootstrap != "request" {
		t.Fatalf("the joiner's admin init: status %d, out %q, stderr %q, bootstrap %q", status, out, errOut, event.Policy.Bootstrap)
	}
	if out, _, _, _ := lit(owner, "get", "/members/requests"); !strings.Contains(out, principal(joiner)) {
		t.Errorf("get /members/requests is %q; want the joiner", out)
	}
	if out, errOut, status, _ := lit(owner, "add", "/members", "{principal: \""+principal(joiner)+"\", role: contributor}"); status != 0 || !strings.Contains(out, "policy version 2 in force") {
		t.Fatalf("admitting the joiner: status %d, out %q, stderr %q", status, out, errOut)
	}
	if out, _, _, _ := lit(owner, "get", "/members"); !strings.Contains(out, principal(joiner)+" contributor") {
		t.Errorf("get /members is %q; want the joiner a contributor", out)
	}
	echo, _, _, _ := lit(owner, "add", "unassigned/children", "{title: launch}")
	epic, _, _ := strings.Cut(strings.TrimPrefix(echo, "+ "), "\n")
	if out, errOut, status, _ := lit(joiner, "add", "unfiled/children", "{title: mine}"); status != 0 {
		t.Fatalf("the contributor files a feature: status %d, out %q, stderr %q", status, out, errOut)
	}

	// 3. the contributor's rm of the epic is refused at the write, naming
	// the capability it needed, and nothing is written
	out, errOut, status, event = lit(joiner, "rm", epic)
	var carried verbs.CarriedEvent
	if failed, ok := event.Outcome.(cli.Failed); !ok || !errors.As(failed.Failure, &carried) || !isUnauthorized(carried.Why) || failed.Failure.Reason() != "unauthorized" {
		t.Errorf("the contributor's rm %s ended %#v; want the fold to carry it as unauthorized", epic, event.Outcome)
	}
	if status == 0 || !strings.Contains(errOut, "rm own/* (level=1)") || len(event.Store.Appends) != 0 {
		t.Errorf("the contributor's rm %s: status %d, out %q, stderr %q", epic, status, out, errOut)
	}
	if v := event.Policy.Verdicts; len(v) != 1 || !carriedUnauthorized(v[0].Outcome) {
		t.Errorf("the refused rm's verdicts are %+v; want the one event carried as unauthorized", v)
	}
	// a modified client appends it anyway: every replica carries it, and
	// the owner's /integrity names the event and its writer
	if err := joiner.Forge(verbs.Rm, verbs.Operand{Path: epic}); err != nil {
		t.Fatal(err)
	}
	out, _, _, _ = lit(owner, "get", "/integrity")
	if !strings.Contains(out, "by "+principal(joiner)) || !strings.Contains(out, ": unauthorized") {
		t.Errorf("get /integrity is %q; want the forged rm and its writer", out)
	}
	if out, _, status, _ := lit(owner, "get", epic+"/title"); status != 0 || out != "launch\n" {
		t.Errorf("after the forged rm the epic reads %q, status %d; want it whole", out, status)
	}
}
