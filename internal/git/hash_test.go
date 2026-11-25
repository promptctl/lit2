package git_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/git"
)

// TestHashBlobEmpty pins the id git gives the empty blob in SHA-1, the one
// every reader of a missing file's content is compared against.
func TestHashBlobEmpty(t *testing.T) {
	const sha1Empty = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	if got := git.HashBlob(git.SHA1, nil).String(); got != sha1Empty {
		t.Errorf("SHA-1 empty blob = %s, want %s", got, sha1Empty)
	}
}
