package field

import (
	"errors"
	"testing"
)

// TestHoldsComparesAsTheKindWrites: an item stores a value in its kind's
// canonical form, so a test's value meets it however the test spelled it.
func TestHoldsComparesAsTheKindWrites(t *testing.T) {
	stored := Found{"1"}
	for atom, want := range map[string]Truth{
		"est=1.0":  true,
		"est=01":   true,
		"est!=1.0": false,
		"est=2,1":  true,
		"est=many": false,
	} {
		a, err := ParseAtom(atom)
		if err != nil {
			t.Fatal(err)
		}
		if got := a.Holds(Number{}, stored); got != want {
			t.Errorf("%s holds %v against a number field storing 1, want %v", atom, got, want)
		}
	}
}

// TestAPatternIsAWholeValue: a pattern kind accepts a value it matches from
// end to end, and nothing that merely contains a match.
func TestAPatternIsAWholeValue(t *testing.T) {
	p, err := NewPattern("[0-9]+")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Parse("123"); err != nil {
		t.Errorf("123 is all digits: %v", err)
	}
	for _, v := range []string{"abc1", "1abc", ""} {
		if _, err := p.Parse(v); !errors.Is(err, errNoMatch) {
			t.Errorf("[0-9]+ Parse(%q) = %v; want errNoMatch", v, err)
		}
	}
	if _, err := NewPattern("(a)|(b)"); err != nil {
		t.Errorf("control NewPattern((a)|(b)): %v", err)
	}
	if _, err := NewPattern("a)|(b"); !errors.Is(err, errNotARegexp) {
		t.Errorf("NewPattern(a)|(b) = %v; want errNotARegexp: it is not a regular expression alone, so it cannot slip out of the anchors", err)
	}
}

// TestAnEnumReadsOnlyItsValues: an enum reads a value it declares and
// refuses one it does not.
func TestAnEnumReadsOnlyItsValues(t *testing.T) {
	e, err := NewEnum([]string{"1", "2", "3", "5", "8"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := e.Parse("5"); err != nil || got != "5" {
		t.Errorf("Parse(5) = %q, %v; want 5", got, err)
	}
	if _, err := e.Parse("13"); !errors.Is(err, ErrNotOneOf) {
		t.Errorf("Parse(13) = %v; want ErrNotOneOf", err)
	}
}
