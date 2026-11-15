package git_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
)

// TestHashBlobMatchesGit holds HashBlob to the oracle lit never calls: git's
// own hash-object, in a repository of each object format, over the bytes of
// a file on disk.
func TestHashBlobMatchesGit(t *testing.T) {
	contents := []string{"", "[workspace]\nprefix = \"lit\"\n", "a\x00b\nc"}
	for _, f := range []git.ObjectFormat{git.SHA1, git.SHA256} {
		dir := gittest.Physical(t)
		gittest.Git(t, dir, "init", "-q", "--object-format="+f.String())
		for _, content := range contents {
			path := filepath.Join(dir, "workspace.toml")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			want := gittest.Git(t, dir, "hash-object", "--", path)
			if got := git.HashBlob(f, []byte(content)).String(); got != want {
				t.Errorf("%s: HashBlob(%q) = %s, git hash-object = %s", f, content, got, want)
			}
		}
	}
}

// TestHashBlobEmpty pins the id git gives the empty blob in SHA-1, the one
// every reader of a missing file's content is compared against.
func TestHashBlobEmpty(t *testing.T) {
	const sha1Empty = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	if got := git.HashBlob(git.SHA1, nil).String(); got != sha1Empty {
		t.Errorf("SHA-1 empty blob = %s, want %s", got, sha1Empty)
	}
}
