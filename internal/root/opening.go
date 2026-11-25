package root

import (
	"fmt"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// placeOf is where p's invocation runs: the checkout p locates, with the
// writer its identity files hold, or where no checkout was found. Main
// and the twin both read it here, over their own ports, so the identity a
// twin-backed test runs under is read as lit2 reads it on disk.
func placeOf(p Process) command.Place { return identified(p, p.Locate()) }

// identified is the place of what git found, l: a checkout with the writer
// its identity files hold, or where no checkout was found.
func identified(p Process, l command.Located) command.Place {
	switch l := l.(type) {
	case command.AtCheckout:
		return command.InCheckout{Checkout: l.Checkout, Identity: readIdentity(p.IdentityFiles(l.Checkout))}
	case command.NoCheckout:
		return l
	case command.Unlocatable:
		return l
	}
	panic(fmt.Sprintf("root: %T is not a Located", l))
}

// writer is a minted checkout, its writer, and its store, opened for that
// writer: the three made together from one command.Opens by writerOf, so
// nothing pairs one with another's `[LAW:types-are-the-program]`.
type writer struct {
	checkout git.Checkout
	id       command.Minted
	store    *store.Store
}

// writerOf is o's writer, with its store over p's ports: how a run opens
// the store its place opens.
func (p Process) writerOf(o command.Opens) writer {
	return writer{checkout: o.Checkout, id: o.Writer, store: store.Open(o.Writer.Checkout, p.Stores(o.Checkout), store.DefaultSegmentBound)}
}
