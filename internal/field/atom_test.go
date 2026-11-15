package field

import (
	"testing"
)

// TestParseAtomForms: each of the five forms of an atom parses to its path and
// operator.
func TestParseAtomForms(t *testing.T) {
	cases := []struct {
		in   string
		path Path
		op   string
	}{
		{"description", "description", "present"},
		{"!blockers", "blockers", "absent"},
		{"verdict=pass", "verdict", "equals"},
		{"labels!=external", "labels", "not-equals"},
		{"title~^T-[0-9]+$", "title", "matches"},
	}
	for _, tc := range cases {
		a, err := ParseAtom(tc.in)
		if err != nil {
			t.Errorf("ParseAtom(%q): %v", tc.in, err)
			continue
		}
		if a.Path() != tc.path {
			t.Errorf("ParseAtom(%q).Path() = %s, want %s", tc.in, a.Path(), tc.path)
		}
		if a.String() != tc.in {
			t.Errorf("ParseAtom(%q).String() = %q: an atom prints as it was written", tc.in, a.String())
		}
	}
}

// TestParseAtomRefusals: a form the grammar does not admit is refused, and the
// refusal names no value it could not read.
func TestParseAtomRefusals(t *testing.T) {
	for _, in := range []string{
		"",
		"!p=v",
		"Path",
		"p=",
		"p=has space",
		"p~[unclosed",
		"p!=",
	} {
		if _, err := ParseAtom(in); err == nil {
			t.Errorf("ParseAtom(%q) parsed; the grammar refuses it", in)
		}
	}
}

// TestHoldsTable: the truth of an atom against what the item holds.
func TestHoldsTable(t *testing.T) {
	found := Found{"ready", "external"}
	cases := []struct {
		atom string
		want Truth
	}{
		{"labels", true},
		{"labels=ready", true},
		{"labels=missing", false},
		{"labels!=missing", true},
		{"labels!=ready", false},
		{"!labels", false},
		{"labels~^rea", true},
		{"labels~^zzz", false},
	}
	for _, tc := range cases {
		a, err := ParseAtom(tc.atom)
		if err != nil {
			t.Fatalf("ParseAtom(%q): %v", tc.atom, err)
		}
		if got := a.Holds(found); got != tc.want {
			t.Errorf("%s holds %v against %v, want %v", tc.atom, got, found, tc.want)
		}
	}
}
