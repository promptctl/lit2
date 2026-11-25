// Package storetest is the helpers every test of a store shares: batches
// to append, appends and publishes that fail the test unless they land,
// and the records a stream reads back. They speak of logs, segments,
// publications, and refs (architecture.md §3.16), never of file layouts.
package storetest

import (
	"fmt"
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

// Batch is one batch of the records rs.
func Batch(t *testing.T, rs ...string) store.Batch {
	t.Helper()
	records := make([]store.Record, len(rs))
	for i, r := range rs {
		records[i] = store.Record(r)
	}
	bt, err := store.NewBatch(records...)
	if err != nil {
		t.Fatal(err)
	}
	return bt
}

// Texts is bt's records, as text.
func Texts(bt store.Batch) []string {
	var ts []string
	for _, r := range bt.Records() {
		ts = append(ts, string(r))
	}
	return ts
}

// AppendAll appends each batch in turn, failing t unless each is
// Appended.
func AppendAll(t testing.TB, s *store.Store, bs ...store.Batch) []store.Appended {
	t.Helper()
	out := make([]store.Appended, len(bs))
	for i, bt := range bs {
		got, ok := s.Append(bt).(store.Appended)
		if !ok {
			t.Fatalf("append %d = %#v; want Appended", i, got)
		}
		out[i] = got
	}
	return out
}

// Numbered is n batches of one record each, prefix-0000 on.
func Numbered(t *testing.T, n int, prefix string) []store.Batch {
	t.Helper()
	bs := make([]store.Batch, n)
	for i := range bs {
		bs[i] = Batch(t, fmt.Sprintf("%s-%04d", prefix, i))
	}
	return bs
}

// Heads is s's heads, failing t on an error or an unreadable stream.
func Heads(t *testing.T, s *store.Store) store.Heads {
	t.Helper()
	h, err := s.Heads()
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Unreadable) != 0 {
		t.Errorf("heads hold unreadable streams: %v", h.Unreadable)
	}
	return h
}

// stream is the stream of ref in h.
func stream(t *testing.T, h store.Heads, ref store.Ref) store.Stream {
	t.Helper()
	for _, s := range h.Streams {
		if s.Ref == ref {
			return s
		}
	}
	t.Fatalf("no stream of %s in %v", ref.Name(), h.Streams)
	return store.Stream{}
}

// Whole reads ref's stream from 0 and returns its scan.
func Whole(t *testing.T, s *store.Store, ref store.Ref) store.Scan {
	t.Helper()
	scan, err := s.Read(store.Range{Stream: stream(t, Heads(t, s), ref), From: 0})
	if err != nil {
		t.Fatal(err)
	}
	return scan
}

// Read is every record of a scan's frames, in order.
func Read(scan store.Scan) []string {
	var ts []string
	for _, f := range scan.Frames {
		ts = append(ts, Texts(f.Batch)...)
	}
	return ts
}

// All is every record of bs, in order.
func All(bs []store.Batch) []string {
	var ts []string
	for _, bt := range bs {
		ts = append(ts, Texts(bt)...)
	}
	return ts
}

// SealOwn seals the store's own log into a publication.
func SealOwn(t *testing.T, s *store.Store) store.Publication {
	t.Helper()
	sealed, err := s.SealOwn()
	if err != nil {
		t.Fatal(err)
	}
	p, ok := sealed.(store.Publication)
	if !ok {
		t.Fatalf("sealing the own log made %#v; want a publication", sealed)
	}
	return p
}

// PublishOwn seals the store's own log and publishes it.
func PublishOwn(t *testing.T, s *store.Store) store.Published {
	t.Helper()
	out, err := s.Publish(SealOwn(t, s))
	if err != nil {
		t.Fatal(err)
	}
	return out
}
