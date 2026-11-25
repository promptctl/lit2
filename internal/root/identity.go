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
// checkout id is no one else's.
func identityFiles(c git.Checkout) (shared, private files.Files) {
	return files.AtMode(command.DirOf(command.PrincipalFile{}, c), identityMode), files.AtMode(command.DirOf(command.IDFile{}, c), identityMode)
}

// identityMode is the mode of both identity files on disk.
const identityMode fs.FileMode = 0o600

// errIdentityUnreadable is a mint refused because an identity file already
// there does not parse: the mint never writes over it.
var errIdentityUnreadable = errors.New("an unreadable identity file")

// MintIdentity creates the checkout id in private and the principal's key
// in shared, each once, then reads both back, so a worktree that loses a
// race to create the clone's key signs with the winner's. It prints
// nothing. When the key file is the one this call created, made has the
// key, before anything is read back: its phrase is shown once, and no
// failure after the file landed, a readback's or a sync's, may lose it
// `[LAW:no-silent-failure]`. A create whose sync failed is landed, and the
// mint goes on; its record says so.
func MintIdentity(shared, private files.Files, entropy io.Reader, made func(events.Key)) (command.Minted, error) {
	var id store.CheckoutID
	if _, err := io.ReadFull(entropy, id[:]); err != nil {
		return command.Minted{}, fmt.Errorf("drawing the checkout id: %w", err)
	}
	key, err := events.NewKey(entropy)
	if err != nil {
		return command.Minted{}, err
	}
	if no, failed := private.CreateOnce(command.NameOf(command.IDFile{}), id.FileText()).(files.NotPlaced); failed {
		return command.Minted{}, fmt.Errorf("creating the checkout id file: %w", no.Why)
	}
	fresh := func(m files.Made) {
		if m == files.Created {
			made(key)
		}
	}
	switch p := shared.CreateOnce(command.NameOf(command.PrincipalFile{}), key.FileText()).(type) {
	case files.NotPlaced:
		return command.Minted{}, fmt.Errorf("creating the key file: %w", p.Why)
	case files.Placed:
		fresh(p.Made)
	case files.PlacedNotDurable:
		fresh(p.Made)
	}
	switch got := readIdentity(shared, private).(type) {
	case command.Minted:
		return got, nil
	case command.Unminted:
		return command.Minted{}, fmt.Errorf("the identity files created are not there: %d missing", len(got.Files()))
	case command.Unreadable:
		return command.Minted{}, fmt.Errorf("%w: %w", errIdentityUnreadable, got.Err)
	}
	panic("root: readIdentity answered no Identity")
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
