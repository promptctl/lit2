package command

import (
	"fmt"
	"strings"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// File is one of a checkout's two identity files (D11).
//
//sumtype:decl
type File interface{ file() }

// IDFile is the checkout id's file, one per checkout in its private dir.
type IDFile struct{}

// PrincipalFile is the principal's key file, one per clone in its shared
// dir, beside the store and outside it.
type PrincipalFile struct{}

func (IDFile) file()        {}
func (PrincipalFile) file() {}

// NameOf is f's name in its directory, which is f's alone, the same in
// every checkout.
func NameOf(f File) files.Name {
	switch f.(type) {
	case IDFile:
		return idName
	case PrincipalFile:
		return principalName
	}
	panic(fmt.Sprintf("command: %T is not a File", f))
}

// The identity files' names.
var (
	idName        = files.MustName("id")
	principalName = files.MustName("principal")
)

// DirOf is the directory f lives in for checkout c, the one its Files is
// over: git names both.
func DirOf(f File, c git.Checkout) files.Dir {
	switch f.(type) {
	case IDFile:
		return c.Private()
	case PrincipalFile:
		return c.Shared()
	}
	panic(fmt.Sprintf("command: %T is not a File", f))
}

// PathOf is f's path for c, as the command event names it.
func PathOf(f File, c git.Checkout) string {
	return DirOf(f, c).PathOf(NameOf(f))
}

// Paths is the missing files' paths for c, the id file's first.
func (u Unminted) Paths(c git.Checkout) []string {
	files := u.Files()
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = PathOf(f, c)
	}
	return paths
}

// Identity is a checkout's writer identity, read from its two files: the
// checkout id and the principal's key. A write needs both, so they are one
// union, not two `[LAW:types-are-the-program]`.
//
//sumtype:decl
type Identity interface{ identity() }

// Minted is a checkout whose two files hold its id and its key.
type Minted struct {
	Checkout store.CheckoutID
	Key      events.Key
}

// Unminted is a checkout missing a file and holding no unreadable one:
// lit2 admin init has not run here.
type Unminted struct{ Missing Missing }

// Unreadable is a file that is present and does not parse, which is not
// the same fact as no file. Err never holds the file's bytes.
type Unreadable struct {
	File File
	Err  error
}

func (Minted) identity()     {}
func (Unminted) identity()   {}
func (Unreadable) identity() {}

// Missing is which identity files are absent: one, or both, never none.
//
//sumtype:decl
type Missing interface{ missing() }

// Only is one file missing.
type Only struct{ File File }

// Both is both files missing.
type Both struct{}

func (Only) missing() {}
func (Both) missing() {}

// Files is the files missing, the id file first.
func (u Unminted) Files() []File {
	switch m := u.Missing.(type) {
	case Only:
		return []File{m.File}
	case Both:
		return []File{IDFile{}, PrincipalFile{}}
	}
	panic(fmt.Sprintf("command: %T is not a Missing", u.Missing))
}

// Opening is what a place opens: a minted checkout's store, for its
// writer, or none, and why, in the words of what the place holds. It is
// the place's, derived by OpeningAt and never set beside it, so whether an
// invocation's store opened is said once `[LAW:one-source-of-truth]`: the
// root opens the store by it and the line says its opening by it, or by
// the writer admin init minted, which no place read before init names.
//
//sumtype:decl
type Opening interface{ opening() }

// Opens is a minted checkout, whose store opens for its writer.
type Opens struct {
	Checkout git.Checkout
	Writer   Minted
}

// NoStore is a place that opens no store: outside a checkout, in one not
// yet minted, or one whose writer cannot be read.
type NoStore struct{ Why error }

func (Opens) opening()   {}
func (NoStore) opening() {}

// OpeningAt is what p opens.
func OpeningAt(p Place) Opening {
	switch p := p.(type) {
	case InCheckout:
		switch id := p.Identity.(type) {
		case Minted:
			return Opens{Checkout: p.Checkout, Writer: id}
		case Unminted:
			return NoStore{Why: fmt.Errorf("this checkout has no identity, %s missing: lit2 admin init mints it", strings.Join(id.Paths(p.Checkout), " and "))}
		case Unreadable:
			return NoStore{Why: fmt.Errorf("reading %s: %w", PathOf(id.File, p.Checkout), id.Err)}
		}
		panic(fmt.Sprintf("command: %T is not an Identity", p.Identity))
	case NoCheckout:
		return NoStore{Why: git.NotFound{Dir: p.Dir}}
	case Unlocatable:
		return NoStore{Why: p.Err}
	}
	panic(fmt.Sprintf("command: %T is not a Place", p))
}
