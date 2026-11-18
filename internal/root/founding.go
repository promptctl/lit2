package root

import (
	"errors"
	"fmt"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/reason"
	"github.com/promptctl/lit2/internal/store"
)

// A store's founding is named by the clone, never by the events, which any
// writer can mint to sort first (§3.17): the pin, in the clone's shared dir
// beside its key, seeds every fold of its store, and is written once, by
// the init that founds the store or adopts the committed FoundingFile.
const (
	pinFile      store.Path = "founding"
	FoundingFile store.Path = ".lit/founding"
)

// pinned is the founding the clone's pin names, NoFounding before init
// writes one.
func pinned(shared store.Files) (fold.Founding, error) {
	id, err := read(shared, pinFile, events.ReadEventIDFile)
	var none store.NotFound
	switch {
	case errors.As(err, &none):
		return fold.NoFounding{}, nil
	case err != nil:
		return nil, storeFailure{err: fmt.Errorf("reading the founding pin: %w", err)}
	}
	return fold.FoundedBy{Enrollment: id}, nil
}

// pin names e the clone's founding unless a pin exists, and reads it
// back, so a race to pin seeds every fold with the winner's.
func pin(shared store.Files, e events.EventID) (fold.FoundedBy, error) {
	if _, err := shared.CreateOnce(pinFile, e.FileText()); err != nil {
		return fold.FoundedBy{}, storeFailure{err: fmt.Errorf("writing the founding pin: %w", err)}
	}
	f, err := pinned(shared)
	if err != nil {
		return fold.FoundedBy{}, err
	}
	return f.(fold.FoundedBy), nil
}

// foundingFileIn is the founding FoundingFile names in tree, NoFounding
// when there is none.
func foundingFileIn(tree store.Files) (fold.Founding, error) {
	id, err := read(tree, FoundingFile, events.ReadEventIDFile)
	var none store.NotFound
	switch {
	case errors.As(err, &none):
		return fold.NoFounding{}, nil
	case err != nil:
		return nil, workspaceFailure{err: fmt.Errorf("reading %s: %w", FoundingFile, err)}
	}
	return fold.FoundedBy{Enrollment: id}, nil
}

// writeFoundingFile writes FoundingFile naming f into tree unless the file
// exists, and refuses a file that names another founding: the pin stands,
// and the file is what the next clone adopts.
func writeFoundingFile(tree store.Files, f fold.FoundedBy) error {
	if _, err := tree.CreateOnce(FoundingFile, f.Enrollment.FileText()); err != nil {
		return workspaceFailure{err: fmt.Errorf("writing %s: %w", FoundingFile, err)}
	}
	got, err := foundingFileIn(tree)
	if err != nil {
		return err
	}
	if got != fold.Founding(f) {
		return foundingContradicted{pinned: f, file: got.(fold.FoundedBy)}
	}
	return nil
}

// foundingRaced is a founding enrollment another checkout of the clone
// pinned first: its store is the clone's, which init joins when run again.
type foundingRaced struct{ won fold.FoundedBy }

func (f foundingRaced) Error() string {
	return fmt.Sprintf("another checkout of this clone founded its store first, by enrollment %s: lit2 admin init again joins it", f.won.Enrollment)
}
func (foundingRaced) Class() reason.Class { return reason.ClassEnvironment }
func (foundingRaced) Reason() string      { return "founding" }

// foundingContradicted is a FoundingFile naming another founding than the
// clone's pin: the file is the environment's to restore.
type foundingContradicted struct{ pinned, file fold.FoundedBy }

func (c foundingContradicted) Error() string {
	return fmt.Sprintf("this clone's store was founded by enrollment %s, and %s names %s: restore %s to name %s",
		c.pinned.Enrollment, FoundingFile, c.file.Enrollment, FoundingFile, c.pinned.Enrollment)
}
func (foundingContradicted) Class() reason.Class { return reason.ClassEnvironment }
func (foundingContradicted) Reason() string      { return "founding" }
