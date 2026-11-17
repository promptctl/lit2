package field

import "testing"

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
		if _, err := p.Parse(v); err == nil {
			t.Errorf("%q is accepted by [0-9]+", v)
		}
	}
	if _, err := NewPattern("a)|(b"); err == nil {
		t.Error("a)|(b is not a regular expression alone, so it cannot slip out of the anchors")
	}
}
