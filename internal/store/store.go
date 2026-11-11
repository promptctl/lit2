// Package store is lit's bytes under the common dir: the log, segments, refs
// and snapshot.
//
// Its layer in architecture.md §6: the append-only log; sealing into
// segments; one commit and one ref per writer, inbox refs; rewrites;
// snapshot; the background sync process (§3.13, §3.16, §5 steps 1, 6 and 9).
// Its root is <git-common-dir>/lit/store/ (D11). Here it holds the checkout
// id, the 128-bit token writer refs and claims key on (§3.14).
package store

import (
	"bytes"
	"encoding/hex"
	"fmt"
)

// CheckoutID is the 128-bit token naming one writing checkout.
type CheckoutID [16]byte

// ParseCheckoutID reads a checkout id written as 32 lowercase hex digits,
// the one form String writes, so an id has one spelling.
func ParseCheckoutID(s string) (CheckoutID, error) {
	var id CheckoutID
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(id) || hex.EncodeToString(b) != s {
		return CheckoutID{}, fmt.Errorf("checkout id %q is not 32 lowercase hex digits", s)
	}
	copy(id[:], b)
	return id, nil
}

// String writes the id as 32 lowercase hex digits.
func (id CheckoutID) String() string { return hex.EncodeToString(id[:]) }

// ReadCheckoutIDFile reads the id file's form (D11), exactly: the id's 32
// lowercase hex digits and one newline. It is FileText's inverse.
func ReadCheckoutIDFile(b []byte) (CheckoutID, error) {
	text, ok := bytes.CutSuffix(b, []byte("\n"))
	if !ok {
		return CheckoutID{}, fmt.Errorf("checkout id file %q does not end in a newline", b)
	}
	return ParseCheckoutID(string(text))
}

// FileText is the id in the id file's form, which ReadCheckoutIDFile reads.
func (id CheckoutID) FileText() []byte { return []byte(id.String() + "\n") }
