package assets

import (
	"bytes"
	"testing"
)

// TestEveryDeclaredAssetIsEmbedded: each handle the package declares reads its
// file from the binary, so no shipped file is missing at run time.
func TestEveryDeclaredAssetIsEmbedded(t *testing.T) {
	if len(declared) == 0 {
		t.Fatal("no asset is declared")
	}
	for _, a := range declared {
		if len(a.Bytes()) == 0 {
			t.Errorf("%s is embedded empty", a.path)
		}
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
