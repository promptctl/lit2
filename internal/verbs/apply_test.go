package verbs_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/verbs"
)

// apply admits version onto the book's state as w, having read seen.
func (b *book) apply(w store.CheckoutID, seen fold.State, version string) {
	b.t.Helper()
	act, err := verbs.Admit(seen, []byte(version), git.SHA1, running)
	if err != nil {
		b.t.Fatal(err)
	}
	b.mint(w, seen, []events.Draft{act})
}

// TestTwoAppliesOverOneParentContendUntilOneNamesBoth: the board names the
// version contending with the one in force and its writer, and the next
// apply names every live apply, which resolves them (§3.15).
func TestTwoAppliesOverOneParentContendUntilOneNamesBoth(t *testing.T) {
	b := newBook(t, profile(""))
	b.apply(one, b.st, profile(""))
	seen := b.st
	a, c := profile("")+"# a\n", profile("")+"# b\n"
	b.apply(one, seen, a)
	b.apply(two, seen, c)
	in, ok := b.st.Workspace().(fold.InForce)
	if !ok || len(in.Contending) != 1 || len(in.Live) != 2 {
		t.Fatalf("two applies over one parent fold to %#v; want one in force, one contending", b.st.Workspace())
	}
	line := verbs.Workspace(b.st, verbs.OnDisk{Blob: git.HashBlob(git.SHA1, []byte(a))})
	if !strings.Contains(line, "contended by "+in.Contending[0].Blob.String()[:7]+", by ") || !strings.Contains(line, "lit2 config commit resolves") {
		t.Errorf("the board says %q; want the contending blob named", line)
	}
	act, err := verbs.Admit(b.st, []byte(a), git.SHA1, running)
	if err != nil {
		t.Fatal(err)
	}
	if over := act.Payload.(events.WorkspaceApply).Over; len(over) != 2 {
		t.Errorf("the resolving apply names %d applies; want both live ones", len(over))
	}
	b.mint(one, b.st, []events.Draft{act})
	if line := verbs.Workspace(b.st, verbs.NoFile{}); strings.Contains(line, "contended") || !strings.Contains(line, "is absent") {
		t.Errorf("after the resolution the board says %q", line)
	}
}

// TestAdmitRefusesAVersionThatDoesNotParse as the version's, nothing
// drafted.
func TestAdmitRefusesAVersionThatDoesNotParse(t *testing.T) {
	if _, err := verbs.Admit(fold.State{}, []byte(profile("")), git.SHA1, running); err != nil {
		t.Fatalf("control: the profile is refused: %v", err)
	}
	_, err := verbs.Admit(fold.State{}, []byte("prefix = [\n"), git.SHA1, running)
	var refused verbs.VersionRefused
	if !errors.As(err, &refused) || len(refused.Refused.Refusals) != 1 || refused.Refused.Refusals[0].Reason != config.ReasonSyntax {
		t.Errorf("a version that does not parse is admitted with %v; want VersionRefused, its one fault syntax", err)
	}
}
