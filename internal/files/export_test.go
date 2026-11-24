package files

import "testing"

// BetweenLookAndOpen has the step run do on each entry's path between its
// look and its open, for t alone, so a test swaps the entry in that gap.
func BetweenLookAndOpen(t *testing.T, do func(path string)) {
	was := betweenLookAndOpen
	betweenLookAndOpen = do
	t.Cleanup(func() { betweenLookAndOpen = was })
}
