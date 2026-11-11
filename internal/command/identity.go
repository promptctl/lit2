package command

import (
	"fmt"
	"path/filepath"

	"github.com/promptctl/lit2/internal/events"
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

// FileOf is where f lives for checkout c: the directory a Files is rooted
// at for it, and its name there, which is f's alone. It is the one place
// each identity path is built.
func FileOf(f File, c git.Checkout) (dir string, name store.Path) {
	switch f.(type) {
	case IDFile:
		return c.PrivateDir(), "id"
	case PrincipalFile:
		return c.SharedDir(), "principal"
	}
	panic(fmt.Sprintf("command: %T is not a File", f))
}

// PathOf is f's path for c, as the command event names it.
func PathOf(f File, c git.Checkout) string {
	dir, name := FileOf(f, c)
	return filepath.Join(dir, filepath.FromSlash(string(name)))
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
