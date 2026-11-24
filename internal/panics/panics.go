// Package panics is how a test holds a call to the panic it names: a
// precondition's panic is an error a test identifies, so a nil dereference
// or any other panic is never taken for it.
package panics

import (
	"errors"
	"testing"
)

// With fails t unless f panics with an error that is want under
// errors.Is. Any other panic is panicked again, so it fails the test as
// itself and never passes for the one named.
func With(t testing.TB, want error, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		if r == nil {
			t.Errorf("no panic; want one that is %v", want)
			return
		}
		if err, ok := r.(error); !ok || !errors.Is(err, want) {
			panic(r)
		}
	}()
	f()
}
