package git

import (
	"errors"
	"strings"
	"testing"
)

// The event carries stderr's tail, git's verdict, and no credential a URL
// held.
func TestScrubKeepsTheVerdictAndDropsCredentials(t *testing.T) {
	if got := scrub([]byte("fatal: unable to access 'https://ann:s3cret@example.com/r.git/': denied\n")); strings.Contains(got, "s3cret") || !strings.Contains(got, "https://***@example.com") {
		t.Errorf("scrub = %q; want the credentials replaced and the rest kept", got)
	}
	if got := scrub([]byte("fatal: 'https://ann:p@ss@example.com/r.git/'")); strings.Contains(got, "ss@") || !strings.Contains(got, "https://***@example.com") {
		t.Errorf("scrub = %q; want a password holding an @ replaced whole", got)
	}
	// a cut that would land inside the URL, after its scheme
	cut := "https://ann:s3cret@example.com/"
	straddled := strings.Repeat("x", stderrCap) + cut[:3] + "\n" + cut + strings.Repeat("y", stderrCap-len(cut)+3)
	if got := scrub([]byte(straddled)); strings.Contains(got, "s3cret") {
		t.Errorf("scrub of a URL the cap cuts kept its password: %q", got[:40])
	}
	long := strings.Repeat("x", 2*stderrCap) + "fatal: the verdict"
	if got := scrub([]byte(long)); len(got) > stderrCap+len("…") || !strings.HasSuffix(got, "fatal: the verdict") {
		t.Errorf("scrub of %d bytes kept %d, ending %q; want at most %d, ending with git's verdict", len(long), len(got), got[len(got)-20:], stderrCap)
	}
}

// lit's repository is created by a git that honors its fsync config, and
// refused by an older one.
func TestAGitOlderThanTheFloorIsRefused(t *testing.T) {
	for version, ok := range map[string]bool{
		"git version 2.50.1 (Apple Git-155)\n": true,
		"git version 2.36.0\n":                 true,
		"git version 3.0.0\n":                  true,
		"git version 2.34.1\n":                 false,
		"git version 1.99.9\n":                 false,
		"hub version 2.14\n":                   false,
	} {
		// the versions at the floor are the control of those below it
		err := atFloor(version)
		if ok && err != nil || !ok && !errors.Is(err, errBelowFloor) {
			t.Errorf("atFloor(%q) = %v; want accepted %v, refused with errBelowFloor", version, err, ok)
		}
	}
}
