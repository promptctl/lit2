package verbs_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold/foldtest"
	"github.com/promptctl/lit2/internal/verbs"
)

// TestAnApprovalStandsUntilAProposalChangesIt: an rm of an approval is
// refused, never a panic.
func TestAnApprovalStandsUntilAProposalChangesIt(t *testing.T) {
	b := newBook(t, profile(""))
	b.st = foldtest.Founded(env, key.Signer(), events.Enroll{Owner: verbs.Owner, Roles: verbs.DefaultRoles()})
	if got := b.refused(verbs.Rm, op("/policy/approvals/abcdef", "")); got != "not_removable" {
		t.Errorf("rm of an approval is refused %s, want not_removable", got)
	}
}

// TestOneCommandProposesOnce: two governed writes in one command, one on a
// member's path, are one proposal too many.
func TestOneCommandProposesOnce(t *testing.T) {
	b := newBook(t, profile(""))
	b.st = foldtest.Founded(env, key.Signer(), events.Enroll{Owner: verbs.Owner, Roles: verbs.DefaultRoles()})
	b.do(verbs.Add, op("/members", `{principal: "`+joiner.Principal().String()+`", role: contributor}`))
	if got := b.refused(verbs.Set, op("/members/"+joiner.Principal().String()+"/role", "editor"), op("/policy/threshold", "1")); got != "twice" {
		t.Errorf("a role move and a threshold in one command are refused %q, want twice", got)
	}
}
