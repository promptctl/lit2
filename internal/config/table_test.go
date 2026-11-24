package config

import (
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/field"
)

// TestFindIsTheOnePlaceRule: a path names its row where the row exists, a
// row elsewhere is NotOnLevel, and a path no row takes is UnknownField
// listing the rows there.
func TestFindIsTheOnePlaceRule(t *testing.T) {
	text := replace(`name = "story"`, "name = \"story\"\n[levels.1.fields.points]\nkind = \"number\"")(minimal)
	text += "\n[checks.sized]\ntests = [{ test = \"points\" }]\n"
	s, err := parseText(t, text)
	if err != nil {
		t.Fatal(err)
	}
	two := level(t, 2)
	tbl := s.Table()
	if r, err := tbl.Find(field.Leaf{}, "points"); err != nil || r.(FieldRow).Decl.Origin != field.Declared {
		t.Errorf("points at a leaf = %v, %v; want level 1's declared field", r, err)
	}
	if r, err := tbl.Find(field.Leaf{}, "sized"); err != nil {
		t.Errorf("a check is a row of every item: %v", err)
	} else if _, ok := r.(CheckRow); !ok {
		t.Errorf("sized = %#v, want a check row", r)
	}
	var notHere NotOnLevel
	if _, err := tbl.Find(field.Node{Level: two}, "status"); !errors.As(err, &notHere) {
		t.Errorf("status on a feature = %v, want NotOnLevel", err)
	}
	if _, err := tbl.Find(field.Node{Level: two}, "points"); !errors.As(err, &notHere) {
		t.Errorf("level 1's points on a feature = %v, want NotOnLevel", err)
	}
	var unknown UnknownField
	if _, err := tbl.Find(field.Root{}, "nope"); !errors.As(err, &unknown) {
		t.Fatalf("nope = %v, want UnknownField", err)
	}
	root := []field.Path{"children", "integrity", "lanes", "members", "members/requests", "next", "policy", "policy/approvals", "policy/proposals", "policy/threshold", "workspace"}
	if !reflect.DeepEqual(unknown.Known, root) {
		t.Errorf("the root's rows are %v, want %v", unknown.Known, root)
	}
	if b, d, c := tbl.Origins(); d != 1 || c != 1 || b == 0 {
		t.Errorf("origins = %d built-in, %d declared, %d checks; want 1 declared and 1 check", b, d, c)
	}
}

// TestPlacesNameOnlyDeclaredHomes: a level without a home has no home
// place, so help never lists fields of a node that cannot exist; a member
// of the policy is a place of every version.
func TestPlacesNameOnlyDeclaredHomes(t *testing.T) {
	s, err := parseText(t, minimal)
	if err != nil {
		t.Fatal(err)
	}
	two := level(t, 2)
	want := []field.Place{field.Root{}, field.Home{Level: two}, field.Node{Level: two}, field.Leaf{}, field.Comment{}, field.MemberEntry{}}
	if got := s.Table().Places(); !slices.Equal(got, want) {
		t.Errorf("places = %#v, want %#v", got, want)
	}
}

// TestALevelMayTakeAName: a comment's built-in fields are no item's, so a
// level may declare a field by one of their names, and each place reads
// its own.
func TestALevelMayTakeAName(t *testing.T) {
	text := replace(`name = "story"`, "name = \"story\"\n[levels.1.fields.by]\nkind = \"text\"")(minimal)
	s, err := parseText(t, text)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := s.Table().Find(field.Leaf{}, "by"); err != nil || r.(FieldRow).Decl.Origin != field.Declared {
		t.Errorf("by at a leaf = %v, %v; want level 1's declared field", r, err)
	}
	if r, err := s.Table().Find(field.Comment{}, "by"); err != nil || r.(FieldRow).Decl.Writability != field.Derived {
		t.Errorf("by at a comment = %v, %v; want the derived writer", r, err)
	}
}
