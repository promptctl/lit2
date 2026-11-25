package root

import (
	"errors"
	"fmt"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/reason"
	"github.com/promptctl/lit2/internal/verbs"
)

// A store's founding is named by the clone, never by the events, which any
// writer can mint to sort first (§3.17): the pin, in the clone's shared
// dir, seeds every fold of its store, and is written once, by the init
// that founds the store or adopts the committed FoundingFile. No command
// moves it: a clone joins another store only when a person removes the
// pin.
var (
	pinFile      = files.MustName("founding")
	foundingFile = files.MustName("founding")
)

// FoundingFile is the founding file's name in the work tree's .lit.
func FoundingFile() files.Name { return foundingFile }

// foundingPath is FoundingFile's path from the work tree's root, as a
// message names it.
func foundingPath() string { return verbs.LitPath(foundingFile) }

// pinDir is the directory of c's pin: the clone's shared dir, which every
// worktree of the clone reads, so one pin seeds every fold of its store.
// It is the pin's own, whatever directory the principal's key is in.
func pinDir(c git.Checkout) files.Dir { return c.Shared() }

// pinFiles is the files of c's pin on disk, a port of its own.
func pinFiles(c git.Checkout) files.Files { return files.At(pinDir(c)) }

// pinPath is the pin's path in c, as a failure names it.
func pinPath(c git.Checkout) string { return pinDir(c).PathOf(pinFile) }

// pinned is the founding the clone's pin, in pins, names, NoFounding
// before init writes one.
func pinned(pins files.Files) (fold.Founding, error) {
	id, err := read(pins, pinFile, events.ReadEventIDFile)
	var none files.NotFound
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
func pin(pins files.Files, e events.EventID) (fold.FoundedBy, error) {
	// a pin whose sync failed is written: the run reads it back and goes
	// on, and ends notDurable from its record
	if no, failed := pins.CreateOnce(pinFile, e.FileText()).(files.NotPlaced); failed {
		return fold.FoundedBy{}, storeFailure{err: fmt.Errorf("writing the founding pin: %w", no.Why)}
	}
	f, err := pinned(pins)
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
func adopt(pins, tree files.Files) error {
	f, err := pinned(pins)
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
		_, err = pin(pins, n.Enrollment)
	}
	return err
}

// foundingFileIn is the founding FoundingFile names in tree, NoFounding
// when there is none.
func foundingFileIn(tree files.Files) (fold.Founding, error) {
	id, err := read(tree, foundingFile, events.ReadEventIDFile)
	var none files.NotFound
	switch {
	case errors.As(err, &none):
		return fold.NoFounding{}, nil
	case err != nil:
		return nil, workspaceFailure{err: fmt.Errorf("reading %s: %w", foundingPath(), err)}
	}
	return fold.FoundedBy{Enrollment: id}, nil
}

// writeFoundingFile writes FoundingFile naming f, the founding pinned at
// pin, into tree unless the file exists, and refuses a file that names
// another founding: the pin stands, and the file is what the next clone
// adopts.
func writeFoundingFile(tree files.Files, f fold.FoundedBy, pin string) error {
	if no, failed := tree.CreateOnce(foundingFile, f.Enrollment.FileText()).(files.NotPlaced); failed {
		return workspaceFailure{err: fmt.Errorf("writing %s: %w", foundingPath(), no.Why)}
	}
	got, err := foundingFileIn(tree)
	if err != nil {
		return err
	}
	switch got := got.(type) {
	case fold.NoFounding:
		return workspaceFailure{err: fmt.Errorf("%s written is not there", foundingPath())}
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
	return fmt.Sprintf("this store was founded by enrollment %s, which it does not hold yet: it arrives when this clone syncs. If %s is not the store this clone joins, remove the pin at %s: %s",
		f.Enrollment, f.Enrollment, f.pin, afterUnpin())
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
	return fmt.Sprintf("this store's founding, enrollment %s, is carried as %s, so no policy says who may write: if its suite is unknown, upgrade lit2; if the pin at %s names the wrong event, remove it: %s",
		f.Enrollment, fold.ReasonName(f.Why), f.pin, afterUnpin())
}
func (foundingCarried) Class() reason.Class { return reason.ClassEnvironment }
func (foundingCarried) Reason() string      { return "policy" }

// afterUnpin is what admin init does in a clone whose pin a person removed.
func afterUnpin() string {
	return "lit2 admin init then adopts the founding " + foundingPath() + " names, or founds the store when there is no file"
}

// unwritten is a founding frame whose append failed after its enrollment
// was pinned: the store does not hold the founding the pin names.
type unwritten struct {
	err error
	pin string
}

func (u unwritten) Error() string {
	return fmt.Sprintf("%v; the pin at %s names the founding this run did not write: remove it, and %s", u.err, u.pin, afterUnpin())
}
func (unwritten) Class() reason.Class { return reason.ClassEnvironment }
func (unwritten) Reason() string      { return "store" }

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
		c.pinned.Enrollment, c.pin, foundingPath(), c.file.Enrollment, c.file.Enrollment)
}
func (foundingContradicted) Class() reason.Class { return reason.ClassEnvironment }
func (foundingContradicted) Reason() string      { return "founding" }
