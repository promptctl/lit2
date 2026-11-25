package root

import (
	"errors"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// placeOf is where p's invocation runs: the checkout p locates, with the
// writer its identity files hold, or the place that is no checkout. Main
// and the twin both read it here, over their own ports, so the identity a
// twin-backed test runs under is read as lit2 reads it on disk.
func placeOf(p Process) command.Place {
	dir, c, err := p.Locate()
	var none git.NotFound
	if errors.As(err, &none) {
		return command.NoCheckout{Dir: none.Dir}
	}
	if err != nil {
		return command.Unlocatable{Dir: dir, Err: err}
	}
	return command.InCheckout{Checkout: c, Identity: readIdentity(p.IdentityFiles(c))}
}

// situation is the store a run holds, opened as its place opens it, never
// set beside the place, so no run holds a store that is not its place's
// `[LAW:one-source-of-truth]`.
//
//sumtype:decl
type situation interface{ situation() }

// held is a place that opens its writer's store.
type held struct{ writer }

// noStore is a place that opens no store, and why.
type noStore struct{ why error }

func (held) situation()    {}
func (noStore) situation() {}

// writer is a minted checkout, its writer, and its store, opened for that
// writer: the three together, so nothing pairs one with another's
// `[LAW:types-are-the-program]`. It is no situation itself, so a run that
// carries one, opened and folded, is not taken for one.
type writer struct {
	checkout git.Checkout
	id       command.Minted
	store    *store.Store
}

// situate is the store p holds at place.
func situate(p Process, place command.Place) (s situation) {
	switch o := command.OpeningAt(place).(type) {
	case command.Opens:
		s = held{p.writerOf(o.Checkout, o.Writer)}
	case command.NoStore:
		s = noStore{why: o.Why}
	}
	return s
}

// writerOf is c minted as id, with its store over p's ports: how a run's
// store is opened, and how admin init opens the store of the checkout it
// has just minted.
func (p Process) writerOf(c git.Checkout, id command.Minted) writer {
	return writer{checkout: c, id: id, store: store.Open(id.Checkout, p.Stores(c), store.DefaultSegmentBound)}
}
