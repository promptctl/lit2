package store

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/promptctl/lit2/internal/git"
)

// InboxID names one inbox: a stream filed by a writer with no checkout here.
type InboxID [16]byte

// String writes the id as 32 lowercase hex digits.
func (id InboxID) String() string { return hex.EncodeToString(id[:]) }

// Ref is a stream's ref in lit's repository, one of exactly two namespaces,
// so any other ref under refs/lit/ is unreadable and never a stream
// `[LAW:types-are-the-program]`.
//
//sumtype:decl
type Ref interface {
	Name() git.RefName
	ref()
}

// WriterRef is a writing checkout's ref, refs/lit/log/<checkout-id>.
type WriterRef struct{ Checkout CheckoutID }

// InboxRef is an inbox's ref, refs/lit/inbox/<inbox-id>.
type InboxRef struct{ Inbox InboxID }

func (WriterRef) ref() {}
func (InboxRef) ref()  {}

// The namespaces under refs/lit/, and refs/lit/ itself.
const (
	litRefs  git.RefName = "refs/lit/"
	writerNS             = litRefs + "log/"
	inboxNS              = litRefs + "inbox/"
)

// Name is refs/lit/log/<checkout-id>.
func (r WriterRef) Name() git.RefName { return writerNS + git.RefName(r.Checkout.String()) }

// Name is refs/lit/inbox/<inbox-id>.
func (r InboxRef) Name() git.RefName { return inboxNS + git.RefName(r.Inbox.String()) }

// ParseRef is the one parser of a ref name into a Ref: a writer's or an
// inbox's, its id in the one spelling String writes. Anything else is
// refused `[LAW:parse-dont-validate]`.
func ParseRef(n git.RefName) (Ref, error) {
	if s, ok := strings.CutPrefix(string(n), string(writerNS)); ok {
		if id, ok := token(s); ok {
			return WriterRef{Checkout: id}, nil
		}
	}
	if s, ok := strings.CutPrefix(string(n), string(inboxNS)); ok {
		if id, ok := token(s); ok {
			return InboxRef{Inbox: id}, nil
		}
	}
	return nil, fmt.Errorf("%s is neither refs/lit/log/<checkout-id> nor refs/lit/inbox/<inbox-id>", n)
}

// token reads a 128-bit token in its one spelling, 32 lowercase hex digits.
func token(s string) ([16]byte, bool) {
	var t [16]byte
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(t) || hex.EncodeToString(b) != s {
		return t, false
	}
	copy(t[:], b)
	return t, true
}
