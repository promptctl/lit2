package store_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/store"
)

// texts is every record of bs, in order, as text.
func texts(bs ...store.Batch) []string {
	var ts []string
	for _, bt := range bs {
		for _, r := range bt.Records() {
			ts = append(ts, string(r))
		}
	}
	return ts
}

// numbered is n batches of one record each, prefix-0000 on.
func numbered(t testing.TB, n int, prefix string) []store.Batch {
	t.Helper()
	bs := make([]store.Batch, n)
	for i := range bs {
		bs[i] = store.BatchOf(t, fmt.Sprintf("%s-%04d", prefix, i))
	}
	return bs
}

// headsOf is s's heads, failing t on an error or an unreadable stream.
func headsOf(t *testing.T, s *store.Store) store.Heads {
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

// whole is ref's stream in s, read from 0.
func whole(t *testing.T, s *store.Store, ref store.Ref) store.Scan {
	t.Helper()
	h := headsOf(t, s)
	i := slices.IndexFunc(h.Streams, func(st store.Stream) bool { return st.Ref == ref })
	if i < 0 {
		t.Fatalf("no stream of %s in %v", ref.Name(), h.Streams)
	}
	scan, err := s.Read(store.Range{Stream: h.Streams[i], From: 0})
	if err != nil {
		t.Fatal(err)
	}
	return scan
}

// sealOwn is s's own log sealed into a publication.
func sealOwn(t *testing.T, s *store.Store) store.Publication {
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

// publishOwn is s's own log sealed and published.
func publishOwn(t *testing.T, s *store.Store) store.Published {
	t.Helper()
	out, err := s.Publish(sealOwn(t, s))
	if err != nil {
		t.Fatal(err)
	}
	return out
}
