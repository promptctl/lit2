package fold_test

import (
	"testing"

	"github.com/promptctl/lit2/internal/store"
)

// Two records of one batch can hold the same unreadable bytes. Each is its own
// malformed record, so the integrity record and the count agree on two.
func TestTwoIdenticalMalformedRecordsInOneBatchAreTwoEntries(t *testing.T) {
	w := newWorld(t)
	junk := store.Record([]byte{0xff, 0x01})
	w.append(a.ref(), must(store.NewBatch(junk, junk)))
	got := w.replay()
	if n := len(got.State.Integrity().Malformed); n != 2 || got.Facts.Decoded.Malformed != 2 {
		t.Fatalf("two identical malformed records are %d integrity entries and a count of %d; want 2 and 2", n, got.Facts.Decoded.Malformed)
	}
}
