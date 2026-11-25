// Package storetest is what a test outside the store needs of one:
// entropy for its ports, and appends that fail the test unless they land.
package storetest

import (
	"io"
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/promptctl/lit2/internal/store"
)

// Entropy is seeded entropy safe for concurrent reads, for a store's
// ports: logs created at once by many appenders each read an id.
func Entropy(seed byte) io.Reader { return &locked{r: rand.NewChaCha8([32]byte{seed})} }

type locked struct {
	mu sync.Mutex
	r  io.Reader
}

func (l *locked) Read(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Read(b)
}

// AppendAll appends each batch in turn, failing t unless each is
// Appended.
func AppendAll(t testing.TB, s *store.Store, bs ...store.Batch) []store.Appended {
	t.Helper()
	out := make([]store.Appended, len(bs))
	for i, bt := range bs {
		got := s.Append(bt)
		a, ok := got.(store.Appended)
		if !ok {
			t.Fatalf("append %d = %#v; want Appended", i, got)
		}
		out[i] = a
	}
	return out
}
