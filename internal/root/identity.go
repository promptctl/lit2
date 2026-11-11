package root

import (
	"errors"
	"fmt"
	"io"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// identityFiles is c's two identity directories on disk, through the
// store's one create-once port, each file 0600: the principal's key is a
// secret, and the checkout id is no one else's.
func identityFiles(c git.Checkout) (shared, private store.Files) {
	return store.FilesAtMode(command.DirOf(command.PrincipalFile{}, c), 0o600), store.FilesAtMode(command.DirOf(command.IDFile{}, c), 0o600)
}

// Fresh is what MintIdentity did to each file: created it, or found it
// there already. init prints the recovery phrase only for a Created key.
type Fresh struct{ Checkout, Key store.Made }

// MintIdentity creates the checkout id in private and the principal's key
// in shared, each once, then reads both back, so a worktree that loses a
// race to create the clone's key signs with the winner's. It is init's
// call; it prints nothing.
func MintIdentity(shared, private store.Files, entropy io.Reader) (command.Minted, Fresh, error) {
	var id store.CheckoutID
	if _, err := io.ReadFull(entropy, id[:]); err != nil {
		return command.Minted{}, Fresh{}, fmt.Errorf("drawing the checkout id: %w", err)
	}
	key, err := events.NewKey(entropy)
	if err != nil {
		return command.Minted{}, Fresh{}, err
	}
	var fresh Fresh
	if fresh.Checkout, err = private.CreateOnce(command.NameOf(command.IDFile{}), id.FileText()); err != nil {
		return command.Minted{}, Fresh{}, fmt.Errorf("creating the checkout id file: %w", err)
	}
	if fresh.Key, err = shared.CreateOnce(command.NameOf(command.PrincipalFile{}), key.FileText()); err != nil {
		return command.Minted{}, Fresh{}, fmt.Errorf("creating the key file: %w", err)
	}
	switch got := readIdentity(shared, private).(type) {
	case command.Minted:
		return got, fresh, nil
	case command.Unminted:
		return command.Minted{}, Fresh{}, fmt.Errorf("the identity files created are not there: %d missing", len(got.Files()))
	case command.Unreadable:
		return command.Minted{}, Fresh{}, fmt.Errorf("an identity file does not read: %w", got.Err)
	}
	panic("root: readIdentity answered no Identity")
}

// readIdentity reads a checkout's two identity files. A file present that
// does not parse is Unreadable, the id file's first, since it is a louder
// fact than one missing; otherwise any missing makes the identity Unminted.
func readIdentity(shared, private store.Files) command.Identity {
	id, idErr := read(private, command.NameOf(command.IDFile{}), store.ReadCheckoutIDFile)
	key, keyErr := read(shared, command.NameOf(command.PrincipalFile{}), events.ParseKeyFile)
	var none store.NotFound
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

// read parses the whole file p in fs: an identity file is created once,
// whole, and never rewritten, so its size and its bytes agree.
func read[T any](fs store.Files, p store.Path, parse func([]byte) (T, error)) (T, error) {
	var none T
	size, err := fs.Size(p)
	if err != nil {
		return none, err
	}
	b, err := fs.ReadAt(p, 0, size)
	if err != nil {
		return none, err
	}
	return parse(b)
}
