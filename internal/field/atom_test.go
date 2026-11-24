package field

import (
	"errors"
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
		{"pr/verdict=pass", "pr/verdict", "equals"},
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

// TestParseAtomRefusals: a form the grammar does not admit is refused as no
// atom, and a regular expression that does not compile as no regular
// expression; each beside a control the grammar reads.
func TestParseAtomRefusals(t *testing.T) {
	for _, c := range []struct {
		in, control string
		want        error
	}{
		{"", "p", errNotAnAtom},
		{"!p=v", "p!=v", errNotAnAtom},
		{"Path", "path", errNotAnAtom},
		{"p=", "p=v", errNotAnAtom},
		{"p=has space", `p="has space"`, errNotAnAtom},
		{"p~[unclosed", "p~[closed]", errNotARegexp},
		{"p!=", "p!=v", errNotAnAtom},
		{"p=a,", "p=a,b", errNotAnAtom},
		{"p=\"open", `p="open"`, errNotAnAtom},
		{"p=\"a\"b", `p="a",b`, errNotAnAtom},
		{"p<3", "p=3", errNotAnAtom},
		{"p=a\nb", "p=\"a\nb\"", errNotAnAtom},
	} {
		if _, err := ParseAtom(c.control); err != nil {
			t.Errorf("control ParseAtom(%q): %v", c.control, err)
		}
		if _, err := ParseAtom(c.in); !errors.Is(err, c.want) {
			t.Errorf("ParseAtom(%q) = %v; want %v", c.in, err, c.want)
		}
	}
}

// TestAtomValues: a quoted value takes "" for a quote and every other
// character as itself, backslashes included, and = and != take several values
// separated by commas, where ~ takes one regular expression, commas and all.
func TestAtomValues(t *testing.T) {
	cases := []struct {
		atom  string
		found Found
		want  Truth
	}{
		{`status=backlog,impl`, Found{"impl"}, true},
		{`status=backlog,impl`, Found{"review"}, false},
		{`status!=backlog,impl`, Found{"review"}, true},
		{`status!=backlog,impl`, Found{"backlog"}, false},
		{`reason="a,b"`, Found{"a,b"}, true},
		{`reason="a,b"`, Found{"a"}, false},
		{`reason="say ""hi"""`, Found{`say "hi"`}, true},
		{`path="C:\dir\file"`, Found{`C:\dir\file`}, true},
		{`reason="a b",c`, Found{"c"}, true},
		{`title~"^\d+$"`, Found{"42"}, true},
		{`title~^a{1,3}$`, Found{"aa"}, true},
		{"description=\"a\nb\"", Found{"a\nb"}, true},
	}
	for _, tc := range cases {
		a, err := ParseAtom(tc.atom)
		if err != nil {
			t.Errorf("ParseAtom(%q): %v", tc.atom, err)
			continue
		}
		if got := a.Holds(Text{}, tc.found); got != tc.want {
			t.Errorf("%s holds %v against %v, want %v", tc.atom, got, tc.found, tc.want)
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
		if got := a.Holds(Text{}, found); got != tc.want {
			t.Errorf("%s holds %v against %v, want %v", tc.atom, got, found, tc.want)
		}
	}
}
