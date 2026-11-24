// Package store is lit's bytes under the common dir: the log, segments and
// refs.
//
// Its layer in architecture.md §6: the append-only log; sealing into
// segments; one commit and one ref per writer, inbox refs; rewrites;
// the background sync process (§3.13, §3.16, §5 steps 1, 6 and 9).
// Its root is <git-common-dir>/lit/store/ (D11). One Store type runs over
// three ports, each with an adapter on disk and a twin in memory: Files
// over its logs' directory, which only the store opens, and lit's
// repository's Objects and Refs.
// It also holds the checkout id, the 128-bit token writer refs and claims
// key on (§3.14).
package store

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
)

// CheckoutID is the 128-bit token naming one writing checkout.
type CheckoutID [16]byte

// The refusals of a checkout id and of its file: text that is not an id,
// and a file not in the id file's form.
var (
	errNotACheckoutID = errors.New("a malformed checkout id")
	errNotIDFileForm  = errors.New("a checkout id file not in its form")
)

// ParseCheckoutID reads a checkout id written as 32 lowercase hex digits,
// the one form String writes, so an id has one spelling.
// Its error names the length, never the text, which is not known to be an
// id and lands on every command event.
func ParseCheckoutID(s string) (CheckoutID, error) {
	id, ok := token(s)
	if !ok {
		return CheckoutID{}, fmt.Errorf("%d bytes that are not a checkout id, 32 lowercase hex digits: %w", len(s), errNotACheckoutID)
	}
	return id, nil
}

// String writes the id as 32 lowercase hex digits.
func (id CheckoutID) String() string { return hex.EncodeToString(id[:]) }

// ReadCheckoutIDFile reads the id file's form (D11), exactly: the id's 32
// lowercase hex digits and one newline. It is FileText's inverse. It
// refuses by length first, and no error carries the file's bytes.
func ReadCheckoutIDFile(b []byte) (CheckoutID, error) {
	if form := 2*len(CheckoutID{}) + 1; len(b) != form {
		return CheckoutID{}, fmt.Errorf("the checkout id file holds %d bytes; its form is %d: %w", len(b), form, errNotIDFileForm)
	}
	text, ok := bytes.CutSuffix(b, []byte("\n"))
	if !ok {
		return CheckoutID{}, fmt.Errorf("the checkout id file does not end in a newline: %w", errNotIDFileForm)
	}
	return ParseCheckoutID(string(text))
}

// FileText is the id in the id file's form, which ReadCheckoutIDFile reads.
func (id CheckoutID) FileText() []byte { return []byte(id.String() + "\n") }
