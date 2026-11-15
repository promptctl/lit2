package config

import (
	"errors"
	"testing"

	"github.com/promptctl/lit2/internal/events"
)

// layered is fullFile, parsed: the root alone, a section that adds review,
// and a home whose claim lifetime is its own.
func layered(t *testing.T) Schema {
	t.Helper()
	s, err := parseText(t, fullFile)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func itemNode(t *testing.T, id string) NodeKey {
	t.Helper()
	parsed, err := events.ParseItemID(id)
	if err != nil {
		t.Fatal(err)
	}
	return ItemNode(parsed)
}

// TestEffectiveRootAlone: an item with no section reads the root's columns and
// the root's claim lifetime, and every value says it came from the root.
func TestEffectiveRootAlone(t *testing.T) {
	e, err := layered(t).Effective(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := e.Columns.Resolve("review"); ok {
		t.Error("review is a section's column; the root alone does not declare it")
	}
	if _, ok := e.ColumnOrigin["backlog"].(RootOrigin); !ok {
		t.Errorf("backlog's origin = %T, want the root", e.ColumnOrigin["backlog"])
	}
	if _, ok := e.TTLOrigin.(RootOrigin); !ok || e.ClaimTTL.String() != "6h0m0s" {
		t.Errorf("claim_ttl = %s from %T, want 6h from the root", e.ClaimTTL, e.TTLOrigin)
	}
}

// TestEffectiveSectionAddsAColumn: a section's column joins the graph with its
// origin named, and a section's claim lifetime takes precedence over the root's.
func TestEffectiveSectionAddsAColumn(t *testing.T) {
	s := layered(t)
	// review's next names audit, which the home declares, so the home is in the ancestry.
	a := Ancestry{itemNode(t, "lit-7k2"), HomeNode("unfiled")}
	e, err := s.Effective(a)
	if err != nil {
		t.Fatal(err)
	}
	review, ok := e.Columns.Resolve("review")
	if !ok {
		t.Fatal("the section's review column is not in the effective graph")
	}
	if review.Stage() != Active {
		t.Errorf("review = %s, want active", review.Stage())
	}
	if origin, ok := e.ColumnOrigin["review"].(SectionOrigin); !ok || origin.Key != a[0] {
		t.Errorf("review's origin = %#v, want the lit-7k2 section", e.ColumnOrigin["review"])
	}
	// lit-7k2 declares no claim_ttl, so the home's 2h is the nearest one that does.
	if e.ClaimTTL.String() != "2h0m0s" {
		t.Errorf("claim_ttl = %s, want the home's 2h", e.ClaimTTL)
	}
}

// TestEffectiveHomeClaimLifetime: a declared home's section sets the claim
// lifetime and says so.
func TestEffectiveHomeClaimLifetime(t *testing.T) {
	e, err := layered(t).Effective(Ancestry{HomeNode("unfiled")})
	if err != nil {
		t.Fatal(err)
	}
	if e.ClaimTTL.String() != "2h0m0s" {
		t.Errorf("claim_ttl = %s, want the home's 2h", e.ClaimTTL)
	}
	if origin, ok := e.TTLOrigin.(SectionOrigin); !ok || origin.Key != HomeNode("unfiled") {
		t.Errorf("TTLOrigin = %#v, want the unfiled section", e.TTLOrigin)
	}
}

// TestEffectiveNextReachesAcrossAncestors: review's next names audit, which
// only the home declares. The item resolves under the full ancestry and is
// refused without the home on it.
func TestEffectiveNextReachesAcrossAncestors(t *testing.T) {
	s := layered(t)
	item := itemNode(t, "lit-7k2")
	_, err := s.Effective(Ancestry{item})
	var refused *ResolutionRefused
	if !errors.As(err, &refused) {
		t.Fatalf("review's next names audit, which the item's ancestry does not declare: want a refusal, got %v", err)
	}
	if refused.Refusals[0].Reason != ReasonNextUndeclared {
		t.Errorf("reason = %s, want next-target-undeclared", refused.Refusals[0].Reason)
	}
	e, err := s.Effective(Ancestry{item, HomeNode("unfiled")})
	if err != nil {
		t.Fatalf("under the full ancestry it resolves: %v", err)
	}
	if _, ok := e.Columns.Resolve("audit"); !ok {
		t.Error("audit is in the effective graph under the full ancestry")
	}
}

// TestEffectiveSecondInitialIsRefused: a section that adds an initial column
// is a valid section alone, and the graph refuses it where it is layered.
func TestEffectiveSecondInitialIsRefused(t *testing.T) {
	text := fullFile + "\n[nodes.unfiled.columns.intake]\ninitial = true\nnext = [\"done\"]\n"
	s, err := parseText(t, text)
	if err != nil {
		t.Fatalf("a section's initial column parses: %v", err)
	}
	_, err = s.Effective(Ancestry{HomeNode("unfiled")})
	var refused *ResolutionRefused
	if !errors.As(err, &refused) {
		t.Fatalf("want a refusal, got %v", err)
	}
	if refused.Refusals[0].Reason != ReasonManyInitial {
		t.Errorf("reason = %s, want many-initial", refused.Refusals[0].Reason)
	}
	if refused.Refusals[0].Key != "nodes.\"unfiled\".columns.intake" && refused.Refusals[0].Key != "nodes.unfiled.columns.intake" {
		t.Errorf("key = %s, want the section that declared intake", refused.Refusals[0].Key)
	}
}
