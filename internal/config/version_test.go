package config

import (
	"errors"
	"strings"
	"testing"
)

// reader reads a stamp a test needs as a version, failing the test if Go
// could not have stamped it.
func reader(t *testing.T, stamp string) ReaderVersion {
	t.Helper()
	r, err := ReadReaderVersion(stamp)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// TestReadReaderVersion: a stamp reads as itself, and a dirty tree is marked
// modified.
func TestReadReaderVersion(t *testing.T) {
	for stamp, want := range map[string]string{
		"v0.4.0": "v0.4.0",
		"v1.2.3-0.20261101120000-abcdef123456+dirty": "v1.2.3-0.20261101120000-abcdef123456 (modified)",
		"v0.0.0-20261111233357-fde3028a2d02":         "v0.0.0-20261111233357-fde3028a2d02",
	} {
		if got := reader(t, stamp).String(); got != want {
			t.Errorf("%s reads as %q, want %q", stamp, got, want)
		}
	}
}

// TestReadReaderVersionRefusesWhatHasNoVersion: a build that cannot say which
// versions it reads does not run.
func TestReadReaderVersionRefusesWhatHasNoVersion(t *testing.T) {
	reader(t, "v0.4.0") // the control: a stamp Go writes for a tagged build
	// why tells the unstamped build from a stamp that is no version, so the
	// refusal of each is its own: either removed, the other's message shows
	for stamp, why := range map[string]string{"": "without a version stamp", "(devel)": "without a version stamp", "not-a-version": `the build stamp "not-a-version" is not a version`} {
		_, err := ReadReaderVersion(stamp)
		if !errors.Is(err, errNoVersion) {
			t.Errorf("ReadReaderVersion(%q) = %v; want errNoVersion", stamp, err)
			continue
		}
		if !strings.Contains(err.Error(), why) || !strings.Contains(err.Error(), "go build ./cmd/lit2") {
			t.Errorf("ReadReaderVersion(%q) = %v; want it to say %q and name the build instruction", stamp, err, why)
		}
	}
}

// TestGateAdmitsByVersion: a gate of none admits any reader; a minimum admits
// a reader at or above it and refuses one below. A build between tags is past
// the tag below it and short of the next, and a build before the first tag is
// short of every release.
func TestGateAdmitsByVersion(t *testing.T) {
	cases := []struct {
		gate   ReaderGate
		stamp  string
		admits bool
	}{
		{NoGate{}, "v0.1.0", true},
		{NoGate{}, "v0.0.0-20261111233357-fde3028a2d02+dirty", true},
		{AtLeast{V: "v0.4.0"}, "v0.4.0", true},
		{AtLeast{V: "v0.4.0"}, "v0.3.9", false},
		{AtLeast{V: "v0.4.0"}, "v0.4.1-0.20261101120000-abcdef123456", true},
		{AtLeast{V: "v0.4.1"}, "v0.4.1-0.20261101120000-abcdef123456", false},
		{AtLeast{V: "v0.0.1"}, "v0.0.0-20261111233357-fde3028a2d02", false},
	}
	for _, c := range cases {
		if got := admits(c.gate, reader(t, c.stamp)); got != c.admits {
			t.Errorf("%#v admits %s = %v, want %v", c.gate, c.stamp, got, c.admits)
		}
	}
}
