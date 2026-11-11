package store

import "testing"

func TestACheckoutIDHasOneSpelling(t *testing.T) {
	const s = "7a1e0f7b112d4c889e015a6b3c2f4099"
	id, err := ParseCheckoutID(s)
	if err != nil || id.String() != s {
		t.Errorf("ParseCheckoutID(%q) = %v, %v; want it back", s, id, err)
	}
	for _, bad := range []string{"", "7A1E0F7B112D4C889E015A6B3C2F4099", s[:31], s + "00", "7a1e0f7b112d4c889e015a6b3c2f409g", s + "\n"} {
		if _, err := ParseCheckoutID(bad); err == nil {
			t.Errorf("ParseCheckoutID(%q) read an id", bad)
		}
	}
}
