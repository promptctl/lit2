package fold_test

import (
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/store"
)

// garbled is a field.set whose bytes field.set's decoder refuses, so a
// record of it is committed and Unread{Undecodable}.
type garbled struct{}

func (garbled) Kind() events.Kind            { return "field.set" }
func (garbled) Schema() events.SchemaVersion { return 1 }
func (garbled) AppendTo(w *wire.Writer)      { w.Bytes(2, []byte{0xff}) }

// withBody is rec with its body, field 3, replaced by body, or dropped
// when body is nil: the copies a rewrite or a torn write leave.
func withBody(t *testing.T, rec store.Record, body []byte) store.Record {
	t.Helper()
	r := wire.NewReader(rec)
	var w wire.Writer
	for tag, ok := r.Next(); ok; tag, ok = r.Next() {
		b := r.Bytes()
		if tag != 3 {
			w.Bytes(tag, b)
		}
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if body != nil {
		w.Bytes(3, body)
	}
	return store.Record(w.Encoded())
}

// minted is the one record d mints as a, with its event.
func minted(t *testing.T, d events.Draft) store.Record {
	t.Helper()
	src := newWorld(t)
	src.mint(a, fold.State{}, d)
	return src.streams[a.ref()].batches[0].Records()[0]
}

func TestTheCopiesOfOneEventFoldAlikeInEitherArrivalOrder(t *testing.T) {
	id := must(events.ParseItemID("lit-abc"))
	unread := minted(t, events.Act{About: events.Item{ID: id}, Payload: garbled{}})
	readable := minted(t, set(id, title, prose("t")))
	copies := map[string]store.Record{
		"unread":             unread,
		"unread uncommitted": withBody(t, unread, []byte{0xff}),
		"unread erased":      withBody(t, unread, nil),
		"readable":           readable,
		"readable erased":    withBody(t, readable, nil),
	}
	for _, c := range []struct {
		first, second string
		want          func(fold.State) bool
	}{
		{"unread", "unread uncommitted", unappliedFor(fold.Unread{Why: events.Undecodable{}})},
		{"unread", "unread erased", unappliedFor(fold.Unread{Why: events.Undecodable{}})},
		{"unread uncommitted", "unread erased", unappliedFor(fold.Uncommitted{})},
		{"readable", "readable erased", func(s fold.State) bool {
			return len(s.Integrity().Unapplied) == 0 && found(t, s, id).Facts.Text(title) != fold.TextField(fold.Unset{})
		}},
	} {
		w := newWorld(t)
		w.append(a.ref(), must(store.NewBatch(copies[c.first])))
		w.append(b.ref(), must(store.NewBatch(copies[c.second])))
		scans := w.scans()
		one := fold.Genesis(env, fold.Missed{Miss: store.Absent{}}, store.NoSnapshot{}, scans)
		slices.Reverse(scans)
		other := fold.Genesis(env, fold.Missed{Miss: store.Absent{}}, store.NoSnapshot{}, scans)
		if !one.State.Equal(other.State) {
			t.Errorf("%s then %s folds otherwise than the reverse", c.first, c.second)
		}
		if !c.want(one.State) || one.Facts.Duplicates != 1 {
			t.Errorf("%s beside %s keeps %#v with %d duplicates; want the higher-ranked copy", c.first, c.second, one.State.Integrity().Unapplied, one.Facts.Duplicates)
		}
	}
}

// unappliedFor is whether a state's one unapplied event is unapplied for
// why, the reason compared by its kind alone.
func unappliedFor(why fold.UnappliedWhy) func(fold.State) bool {
	return func(s fold.State) bool {
		un := s.Integrity().Unapplied
		if len(un) != 1 {
			return false
		}
		switch w := why.(type) {
		case fold.Unread:
			got, ok := un[0].Why.(fold.Unread)
			if !ok {
				return false
			}
			_, wantUndecodable := w.Why.(events.Undecodable)
			_, isUndecodable := got.Why.(events.Undecodable)
			return wantUndecodable == isUndecodable
		case fold.Erased, fold.Uncommitted, fold.Misaddressed:
			return un[0].Why == why
		}
		return false
	}
}

func TestAnExtendThatReadsNothingKeepsTheSnapshot(t *testing.T) {
	w, _ := item(t)
	first := w.genesis()
	snap, err := fold.ReadSnapshot(wrote(t, first).Payload)
	if err != nil {
		t.Fatal(err)
	}
	kept := fold.Extend(env, snap, wrote(t, first).Marks, nil)
	if kept.Save != fold.Save(fold.Current{}) || kept.Facts.Records != 0 || !kept.State.Equal(first.State) {
		t.Errorf("an extend of nothing saves %#v with %d records; want the snapshot kept, records=0", kept.Save, kept.Facts.Records)
	}
	// a range read to its end is still a read: its marks are written
	if empty := fold.Extend(env, snap, wrote(t, first).Marks, []fold.Scanned{w.span(a.ref(), 1, 1)}); !isWritten(empty.Save) {
		t.Errorf("an extend over an empty range saves %#v; want a snapshot written", empty.Save)
	}
	if again := fold.Genesis(env, fold.Missed{Miss: store.Absent{}}, store.NoSnapshot{}, nil); !isWritten(again.Save) {
		t.Errorf("a genesis of nothing saves %#v; want a snapshot written", again.Save)
	}
}

func isWritten(s fold.Save) bool {
	_, ok := s.(fold.Written)
	return ok
}

// unsigned signs nothing: its signature is zeros under suite, so a test
// makes many events without paying for a signature each.
type unsigned struct {
	events.Signer
	suite events.SuiteID
}

func (u unsigned) Sign([]byte) events.Signature {
	return events.Signature{Suite: u.suite, Bytes: make([]byte, 64)}
}

func TestTheIntegrityRecordGrowsLinearly(t *testing.T) {
	const n, per = 25_000, 1_000
	w := newWorld(t)
	trusted := writer{checkout: checkoutA, signer: unsigned{Signer: keyA.Signer(), suite: events.SuiteEd25519}}
	strange := writer{checkout: checkoutB, signer: unsigned{Signer: keyB.Signer(), suite: 9}}
	for _, wr := range []writer{trusted, strange} {
		for range n / per {
			ds := make([]events.Draft, per)
			for i := range ds {
				ds[i] = events.Act{About: events.Root{}, Payload: unknown{}}
			}
			w.mint(wr, fold.State{}, ds[0], ds[1:]...)
		}
	}
	junk := make([]store.Record, n)
	for i := range junk {
		junk[i] = store.Record([]byte{0xff, byte(i), byte(i >> 8)})
	}
	w.append(b.ref(), must(store.NewBatch(junk...)))
	// A's stream is below a mark, so its zero signatures are trusted and
	// its unknown kinds unapplied; B's suite is unknown, so its are carried
	at := w.span(a.ref(), 0, w.head(a.ref())).Range.Stream
	file := store.Saved{Tag: fold.Tag(), Marks: []store.Mark{{Ref: at.Ref, Upto: at.Until}},
		Payload: wrote(t, fold.Genesis(env, fold.Missed{Miss: store.Absent{}}, store.NoSnapshot{}, nil)).Payload}
	got := fold.Genesis(env, fold.Missed{Miss: store.TagChanged{}}, file, w.scans())
	in := got.State.Integrity()
	if len(in.Unapplied) != n || len(in.Carried) != n || len(in.Malformed) != n {
		t.Fatalf("the fold holds %d unapplied, %d carried, %d malformed; want %d each", len(in.Unapplied), len(in.Carried), len(in.Malformed), n)
	}
	if !slices.IsSortedFunc(in.Unapplied, func(x, y fold.Unapplied) int { return events.Compare(x.Stamp.Order, y.Stamp.Order) }) {
		t.Error("the unapplied events are not in total order")
	}
	snap, err := fold.ReadSnapshot(wrote(t, got).Payload)
	if err != nil || !snap.State.Equal(got.State) || len(snap.Exceptions) != n {
		t.Errorf("the snapshot of %d carried reads back with %d exceptions, %v", n, len(snap.Exceptions), err)
	}
}
