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
