package assets

import (
	"bytes"
	"io/fs"
	"testing"
)

// TestDeclaredAndEmbeddedAreEqual: each handle the package declares reads its
// file from the binary, and every embedded file has a handle, so no shipped
// file is missing at run time or shipped unnamed.
func TestDeclaredAndEmbeddedAreEqual(t *testing.T) {
	named := map[string]bool{}
	for _, a := range declared {
		named[a.path] = true
		if len(a.Bytes()) == 0 {
			t.Errorf("%s is embedded empty", a.path)
		}
	}
	err := fs.WalkDir(files, ".", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !named[path] {
			t.Errorf("%s is embedded and no Asset names it", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestDefaultWorkspaceIsTheShippedFile: the default profile is the workspace
// file this package embeds, so it carries the prefix the shipped file names.
func TestDefaultWorkspaceIsTheShippedFile(t *testing.T) {
	if !bytes.Contains(DefaultWorkspace.Bytes(), []byte("\nprefix = \"lit\"\n")) {
		t.Error("the default workspace names the lit prefix on its own line")
	}
}

// TestBIP39WordlistIsTheStandardList: the English wordlist has the 2048 words
// the standard fixes, one per line.
func TestBIP39WordlistIsTheStandardList(t *testing.T) {
	words := bytes.Split(bytes.TrimSpace(BIP39English.Bytes()), []byte("\n"))
	if len(words) != 2048 {
		t.Errorf("wordlist has %d words, want 2048", len(words))
	}
}
