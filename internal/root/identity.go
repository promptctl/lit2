package root

import (
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// identityFiles is c's two identity directories on disk, each a port of
// its own, each file 0600: the principal's key is a secret, and the
// checkout id is no one else's. Their syncs are kept by u.
func identityFiles(u *files.Durability) func(git.Checkout) (shared, private files.Files) {
	return func(c git.Checkout) (shared, private files.Files) {
		return files.AtMode(command.DirOf(command.PrincipalFile{}, c), identityMode, u), files.AtMode(command.DirOf(command.IDFile{}, c), identityMode, u)
	}
}

// identityMode is the mode of both identity files on disk.
const identityMode fs.FileMode = 0o600

// errIdentityUnreadable is a mint refused because an identity file already
// there does not parse: the mint never writes over it.
var errIdentityUnreadable = errors.New("an unreadable identity file")

// Minting is what MintIdentity did, each with the creates it ran and the
// identity its files held once it ended, read once: minted nothing; found
// the clone's key there; created the key, and read the identity back; or
// created the key and could not read it back. A key this call created is
// carried whatever came after it landed, so its phrase is shown once, and
// no failure after, a readback's or a sync's, may lose it
// `[LAW:no-silent-failure]` `[LAW:types-are-the-program]`. admin init's
// place after its mint is built from that identity, never from a second
// read `[LAW:one-source-of-truth]`.
//
//sumtype:decl
type Minting interface {
	minting()
	creations() []command.Creation
	identity() command.Identity
}

// NotMinted is a mint that placed no identity file of the two, or found
// one that does not read; Read is what the files held after it.
type NotMinted struct {
	Creates []command.Creation
	Read    command.Identity
	Why     error
}

// Found is a mint whose key file was there, the clone's, read back with
// the id.
type Found struct {
	Creates []command.Creation
	Minted  command.Minted
}

// Made is a mint that created the key file, read back with the id.
type Made struct {
	Creates []command.Creation
	Minted  command.Minted
}

// MadeUnread is a mint that created the key file, Key, and could not
// read the identity back; Read is what it read instead.
type MadeUnread struct {
	Creates []command.Creation
	Key     events.Key
	Read    command.Identity
	Why     error
}

func (NotMinted) minting()                         {}
func (n NotMinted) creations() []command.Creation  { return n.Creates }
func (n NotMinted) identity() command.Identity     { return n.Read }
func (Found) minting()                             {}
func (f Found) creations() []command.Creation      { return f.Creates }
func (f Found) identity() command.Identity         { return f.Minted }
func (Made) minting()                              {}
func (m Made) creations() []command.Creation       { return m.Creates }
func (m Made) identity() command.Identity          { return m.Minted }
func (MadeUnread) minting()                        {}
func (m MadeUnread) creations() []command.Creation { return m.Creates }
func (m MadeUnread) identity() command.Identity    { return m.Read }

// MintIdentity creates the checkout id at id and the principal's key at
// key, each once, then reads both back, so a worktree that loses a race to
// create the clone's key signs with the winner's. It prints nothing. A
// create whose sync failed is landed, and the mint goes on; its Creation
// says so. However it ends, it reads the files once, last.
func MintIdentity(id, key onceFile, entropy io.Reader) Minting {
	var creates []command.Creation
	// a mint that stops early says what the files hold all the same
	stopped := func(why error) Minting {
		return NotMinted{Creates: creates, Read: readIdentity(key.files, id.files), Why: why}
	}
	var checkout store.CheckoutID
	if _, err := io.ReadFull(entropy, checkout[:]); err != nil {
		return stopped(fmt.Errorf("drawing the checkout id: %w", err))
	}
	k, err := events.NewKey(entropy)
	if err != nil {
		return stopped(err)
	}
	create := func(f onceFile, b []byte) files.Placing {
		p := f.files.CreateOnce(f.name, b)
		creates = append(creates, command.Creation{Path: f.path(), Placing: p})
		return p
	}
	if no, failed := create(id, checkout.FileText()).(files.NotPlaced); failed {
		return stopped(fmt.Errorf("creating the checkout id file: %w", no.Why))
	}
	var made files.Made
	switch p := create(key, k.FileText()).(type) {
	case files.NotPlaced:
		return stopped(fmt.Errorf("creating the key file: %w", p.Why))
	case files.Placed:
		made = p.Made
	case files.PlacedNotDurable:
		made = p.Made
	}
	read := readIdentity(key.files, id.files)
	var minted command.Minted
	switch got := read.(type) {
	case command.Minted:
		minted = got
	case command.Unminted:
		err = fmt.Errorf("the identity files created are not there: %d missing", len(got.Files()))
	case command.Unreadable:
		err = fmt.Errorf("%w: %w", errIdentityUnreadable, got.Err)
	}
	switch {
	case made == files.Created && err != nil:
		return MadeUnread{Creates: creates, Key: k, Read: read, Why: err}
	case made == files.Created:
		return Made{Creates: creates, Minted: minted}
	case err != nil:
		return NotMinted{Creates: creates, Read: read, Why: err}
	}
	return Found{Creates: creates, Minted: minted}
}

// readIdentity reads a checkout's two identity files. A file present that
// does not parse is Unreadable, the id file's first, since it is a louder
// fact than one missing; otherwise any missing makes the identity Unminted.
func readIdentity(shared, private files.Files) command.Identity {
	id, idErr := read(private, command.NameOf(command.IDFile{}), store.ReadCheckoutIDFile)
	key, keyErr := read(shared, command.NameOf(command.PrincipalFile{}), events.ParseKeyFile)
	var none files.NotFound
	idMissing, keyMissing := errors.As(idErr, &none), errors.As(keyErr, &none)
	switch {
	case idErr != nil && !idMissing:
		return command.Unreadable{File: command.IDFile{}, Err: idErr}
	case keyErr != nil && !keyMissing:
		return command.Unreadable{File: command.PrincipalFile{}, Err: keyErr}
	case idMissing && keyMissing:
		return command.Unminted{Missing: command.Both{}}
	case idMissing:
		return command.Unminted{Missing: command.Only{File: command.IDFile{}}}
	case keyMissing:
		return command.Unminted{Missing: command.Only{File: command.PrincipalFile{}}}
	}
	return command.Minted{Checkout: id, Key: key}
}

// read parses the whole file n in fs.
func read[T any](fs files.Files, n files.Name, parse func([]byte) (T, error)) (T, error) {
	b, err := fs.ReadAll(n)
	if err != nil {
		var none T
		return none, err
	}
	return parse(b)
}
