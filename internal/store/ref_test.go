package store

import (
	"errors"
	"testing"

	"github.com/promptctl/lit2/internal/git"
)

func TestARefNameParsesBackOrIsRefused(t *testing.T) {
	w1, inbox := WriterRef{Checkout: CheckoutID{1}}, InboxRef{Inbox: InboxID{3}}
	for _, r := range []Ref{w1, inbox, WriterRef{Checkout: CheckoutID{0xff, 0x01}}} {
		if back, err := ParseRef(r.Name()); err != nil || back != r {
			t.Errorf("ParseRef(%s) = %v, %v; want it back", r.Name(), back, err)
		}
	}
	for _, n := range []git.RefName{
		Namespace + "log/", Namespace + "log/0A000000000000000000000000000000", Namespace + "log/0a00000000000000000000000000000",
		Namespace + "inbox/0a000000000000000000000000000000/x", Namespace + "other/0a000000000000000000000000000000", "refs/heads/main",
	} {
		if r, err := ParseRef(n); !errors.Is(err, errNotARef) {
			t.Errorf("ParseRef(%s) = %v, %v; want errNotARef", n, r, err)
		}
	}
}
