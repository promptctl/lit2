package config

import (
	"strings"
	"testing"
)

// TestReadReaderVersion: a release stamp is the release; a pseudo-version is a
// development build whose base is the version before it and whose revision is
// the commit's; a dirty tree is marked modified.
func TestReadReaderVersion(t *testing.T) {
	got, err := ReadReaderVersion("v0.4.0")
	if err != nil || got != (Released{V: "v0.4.0"}) {
		t.Errorf("v0.4.0 = %#v, %v; want Released v0.4.0", got, err)
	}
	got, err = ReadReaderVersion("v1.2.3+dirty")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.(Released); !ok {
		t.Errorf("a dirty release is a Released version, got %#v", got)
	}
	got, err = ReadReaderVersion("v0.4.1-0.20261101120000-abcdef123456")
	if err != nil {
		t.Fatal(err)
	}
	if got != (Development{Base: "v0.4.0", Revision: "abcdef123456"}) {
		t.Errorf("pseudo-version = %#v, want Development based on v0.4.0 at abcdef123456", got)
	}
}

// TestReadReaderVersionRefusesWhatHasNoVersion: a build that cannot say which
// versions it reads does not run.
func TestReadReaderVersionRefusesWhatHasNoVersion(t *testing.T) {
	for _, stamp := range []string{"", "(devel)", "not-a-version"} {
		_, err := ReadReaderVersion(stamp)
		if err == nil {
			t.Errorf("ReadReaderVersion(%q) read a version from nothing", stamp)
			continue
		}
		if !strings.Contains(err.Error(), "build") {
			t.Errorf("ReadReaderVersion(%q) names no build instruction: %v", stamp, err)
		}
	}
}

// TestGateAdmitsByVersion: a gate of none admits any reader; a minimum admits
// a reader at or above it and refuses one below.
func TestGateAdmitsByVersion(t *testing.T) {
	if !admits(NoGate{}, Released{V: "v0.1.0"}) {
		t.Error("no gate admits every reader")
	}
	if !admits(AtLeast{V: "v0.4.0"}, Released{V: "v0.4.0"}) {
		t.Error("a reader at the minimum is admitted")
	}
	if admits(AtLeast{V: "v0.4.0"}, Released{V: "v0.3.9"}) {
		t.Error("a reader below the minimum is refused")
	}
	if !admits(AtLeast{V: "v0.4.0"}, Development{Base: "v0.4.1-0.20261101120000-abcdef123456", Revision: "abcdef123456"}) {
		t.Error("a development build reads its base's version")
	}
}
