package root

import (
	"testing"

	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/git"
)

// TestTheConfigBlockNamesTheDefaultAndItsRef: with no committed version, every
// run reads the shipped default, and the event names it by the blob id git
// gives the default's bytes in the store's object format.
func TestTheConfigBlockNamesTheDefaultAndItsRef(t *testing.T) {
	twin := NewTwin()
	_, e := run(t, twin, "help")
	if e.Config == nil {
		t.Fatal("the event has no config block")
	}
	if e.Config.Facts.Source != "default" {
		t.Errorf("config source = %s, want default", e.Config.Facts.Source)
	}
	want := git.HashBlob(git.SHA1, assets.DefaultWorkspace.Bytes()).String()
	if e.Config.Ref != want {
		t.Errorf("config ref = %s, want the default's blob id %s", e.Config.Ref, want)
	}
}

// TestNoStoreRecordsNoRef: a run with no store has no object format to hash
// the version in, so the block names the source and records no ref.
func TestNoStoreRecordsNoRef(t *testing.T) {
	twin := NewTwin()
	twin.Process.Store = command.Unopened{Why: errNoStore{}}
	_, e := run(t, twin, "help")
	if e.Config == nil || e.Config.Facts.Source != "default" {
		t.Fatalf("the block names the default even with no store: %+v", e.Config)
	}
	if e.Config.Ref != "" {
		t.Errorf("ref = %s, want none without a store", e.Config.Ref)
	}
}

type errNoStore struct{}

func (errNoStore) Error() string { return "no store in this test" }
