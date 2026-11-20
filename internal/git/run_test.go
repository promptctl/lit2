package git

import (
	"strings"
	"testing"
)

// The event carries stderr's tail, git's verdict, and no credential a URL
// held.
func TestScrubKeepsTheVerdictAndDropsCredentials(t *testing.T) {
	if got := scrub([]byte("fatal: unable to access 'https://ann:s3cret@example.com/r.git/': denied\n")); strings.Contains(got, "s3cret") || !strings.Contains(got, "https://***@example.com") {
		t.Errorf("scrub = %q; want the credentials replaced and the rest kept", got)
	}
	long := strings.Repeat("x", 2*stderrCap) + "fatal: the verdict"
	if got := scrub([]byte(long)); len(got) > stderrCap+len("…") || !strings.HasSuffix(got, "fatal: the verdict") {
		t.Errorf("scrub of %d bytes kept %d, ending %q; want at most %d, ending with git's verdict", len(long), len(got), got[len(got)-20:], stderrCap)
	}
}
