package root

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/reason"
	"github.com/promptctl/lit2/internal/store"
)

// A store's founding is named by the clone, never by the events, which any
// writer can mint to sort first (§3.17): the pin, in the clone's shared dir
// beside its key, seeds every fold of its store, and is written once, by
// the init that founds the store or adopts the committed FoundingFile. No
// command moves it: a clone joins another store only when a person removes
// the pin.
const (
	pinFile      store.Path = "founding"
	FoundingFile store.Path = ".lit/founding"
)

// pinPath is the pin's path in c, as a failure names it.
func pinPath(c git.Checkout) string { return filepath.Join(c.SharedDir(), string(pinFile)) }

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
	won, ok := f.(fold.FoundedBy)
	if !ok {
		return fold.FoundedBy{}, storeFailure{err: errors.New("the founding pin written is not there")}
	}
	return won, nil
}

// adopt pins the founding FoundingFile in tree names, when the clone pins
// none: a clone adopts the committed founding once.
func adopt(shared, tree store.Files) error {
	f, err := pinned(shared)
	if err != nil {
		return err
	}
	if _, none := f.(fold.NoFounding); !none {
		return nil
	}
	named, err := foundingFileIn(tree)
	if err != nil {
		return err
	}
	if n, names := named.(fold.FoundedBy); names {
		_, err = pin(shared, n.Enrollment)
	}
	return err
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

// writeFoundingFile writes FoundingFile naming f, the founding pinned at
// pin, into tree unless the file exists, and refuses a file that names
// another founding: the pin stands, and the file is what the next clone
// adopts.
func writeFoundingFile(tree store.Files, f fold.FoundedBy, pin string) error {
	if _, err := tree.CreateOnce(FoundingFile, f.Enrollment.FileText()); err != nil {
		return workspaceFailure{err: fmt.Errorf("writing %s: %w", FoundingFile, err)}
	}
	got, err := foundingFileIn(tree)
	if err != nil {
		return err
	}
	switch got := got.(type) {
	case fold.NoFounding:
		return workspaceFailure{err: fmt.Errorf("%s written is not there", FoundingFile)}
	case fold.FoundedBy:
		if got != f {
			return foundingContradicted{pinned: f, file: got, pin: pin}
		}
	}
	return nil
}

// unjudged is why no verb judges by p, the policy of c's store, or nil
// when a version is in force.
func unjudged(p fold.PolicyInForce, c git.Checkout) error {
	switch p := p.(type) {
	case fold.Unenrolled:
		return unenrolled{}
	case fold.FoundingAbsent:
		return foundingAbsent{FoundingAbsent: p, pin: pinPath(c)}
	case fold.FoundedUnread:
		return foundedUnread(p)
	case fold.FoundingCarried:
		return foundingCarried{FoundingCarried: p, pin: pinPath(c)}
	case fold.Enrolled:
		return nil
	}
	panic(fmt.Sprintf("root: %T is not a PolicyInForce", p))
}

// foundingAbsent is a store founded by an enrollment it does not hold:
// no event of it is judged until the founding arrives, or a person removes
// a pin naming one that never will.
type foundingAbsent struct {
	fold.FoundingAbsent
	pin string
}

func (f foundingAbsent) Error() string {
	return fmt.Sprintf("this store was founded by enrollment %s, which it does not hold yet: it arrives when this clone syncs. If %s is not the store this clone joins, remove the pin at %s and lit2 admin init adopts the one %s names",
		f.Enrollment, f.Enrollment, f.pin, FoundingFile)
}
func (foundingAbsent) Class() reason.Class { return reason.ClassEnvironment }
func (foundingAbsent) Reason() string      { return "policy" }

// foundedUnread is a store founded by an enrollment this release cannot
// read: only a release that reads it can judge any event of the store.
type foundedUnread fold.FoundedUnread

func (f foundedUnread) Error() string {
	return fmt.Sprintf("this store was founded by enrollment %s, which this release of lit2 cannot read: upgrade lit2", f.Enrollment)
}
func (foundedUnread) Class() reason.Class { return reason.ClassEnvironment }
func (foundedUnread) Reason() string      { return "policy" }

// foundingCarried is a store whose founding the fold carried: a release
// that knows its suite judges it, and no release judges a forged or
// mistaken one.
type foundingCarried struct {
	fold.FoundingCarried
	pin string
}

func (f foundingCarried) Error() string {
	return fmt.Sprintf("this store's founding, enrollment %s, is carried as %s, so no policy says who may write: if its suite is unknown, upgrade lit2; if the pin at %s names the wrong event, remove it and lit2 admin init adopts the one %s names",
		f.Enrollment, fold.ReasonName(f.Why), f.pin, FoundingFile)
}
func (foundingCarried) Class() reason.Class { return reason.ClassEnvironment }
func (foundingCarried) Reason() string      { return "policy" }

// foundingRaced is a founding enrollment another checkout of the clone
// pinned first: its store is the clone's, which init joins when run again.
type foundingRaced struct{ won fold.FoundedBy }

func (f foundingRaced) Error() string {
	return fmt.Sprintf("another checkout of this clone founded its store first, by enrollment %s: lit2 admin init again joins it", f.won.Enrollment)
}
func (foundingRaced) Class() reason.Class { return reason.ClassEnvironment }
func (foundingRaced) Reason() string      { return "founding" }

// foundingContradicted is a FoundingFile naming another founding than the
// clone's pin: the pin stands, and only a person decides the clone joins
// the store the file names.
type foundingContradicted struct {
	pinned, file fold.FoundedBy
	pin          string
}

func (c foundingContradicted) Error() string {
	return fmt.Sprintf("this clone's store was founded by enrollment %s, which its pin at %s names, and %s names %s: the pin stands. If %s is the store this clone joins, remove the pin and run lit2 admin init again",
		c.pinned.Enrollment, c.pin, FoundingFile, c.file.Enrollment, c.file.Enrollment)
}
func (foundingContradicted) Class() reason.Class { return reason.ClassEnvironment }
func (foundingContradicted) Reason() string      { return "founding" }
