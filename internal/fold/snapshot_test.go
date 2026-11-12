package fold_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/store"
)

func TestASnapshotIsDisposable(t *testing.T) {
	w := generate(t, 7, 30)
	want := w.genesis()
	garbage := []byte("not a fold's payload")
	_, unreadable := fold.ReadSnapshot(garbage)
	if unreadable == nil {
		t.Fatal("ReadSnapshot read bytes no fold wrote")
	}
	torn := errors.New("the snapshot's checksum fails")
	for _, c := range []struct {
		why  fold.Refold
		file store.SnapshotFile
	}{
		{fold.Missed{Miss: store.Absent{}}, store.NoSnapshot{}},
		{fold.Missed{Miss: store.Torn{Why: torn}}, store.TornSnapshot{Why: torn}},
		{fold.PayloadUnreadable{Err: unreadable}, store.Saved{Tag: fold.Tag(), Marks: wrote(t, want).Marks, Payload: garbage}},
	} {
		got := fold.Genesis(env, c.why, c.file, w.scans())
		if !got.State.Equal(want.State) {
			t.Errorf("a refold for %#v disagrees with the genesis", c.why)
		}
		if got.Facts.Base != (fold.Refolded{Why: c.why}) {
			t.Errorf("a refold for %#v reports base %#v", c.why, got.Facts.Base)
		}
		if got.Facts.Trusted != 0 {
			t.Errorf("a refold for %#v trusted %d verdicts it has no snapshot for", c.why, got.Facts.Trusted)
		}
	}
}

// signed is a history of n events from A and B and one forged by B: its
// world, and the forged event.
func signed(t *testing.T) (*world, events.Event) {
	w, id := item(t)
	for i := range 4 {
		w.mint(a, w.state(), set(id, title, prose(fmt.Sprint("a", i))))
		w.mint(b, w.state(), addLabel(id, label("red")))
	}
	forged := w.mint(writer{checkout: checkoutA, signer: forger{keyA.Signer()}}, w.state(), set(id, priority, plain("high")))[0]
	return w, forged
}

func TestAFoldVersionBumpChecksOnlyTheExceptions(t *testing.T) {
	w, forged := signed(t)
	first := w.genesis()
	snap, err := fold.ReadSnapshot(wrote(t, first).Payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Exceptions) != 1 || snap.Exceptions[0] != forged.ID() {
		t.Fatalf("the snapshot's exceptions are %v; want the forged event alone", snap.Exceptions)
	}
	n := first.Facts.Checked
	if n != 10 || first.Facts.Trusted != 0 {
		t.Fatalf("a first genesis checked %d and trusted %d; want all 10 checked", n, first.Facts.Trusted)
	}
	bumped := fold.Genesis(env, fold.Missed{Miss: store.TagChanged{}}, store.Saved{Tag: store.FoldTag("lit2 fold 0"), Marks: wrote(t, first).Marks, Payload: wrote(t, first).Payload}, w.scans())
	if bumped.Facts.Checked != 1 || bumped.Facts.Trusted != n-1 {
		t.Errorf("after a tag change Genesis checked %d and trusted %d; want 1 and %d", bumped.Facts.Checked, bumped.Facts.Trusted, n-1)
	}
	if c := bumped.State.Integrity().Carried; len(c) != 1 || c[0].Event != forged.ID() || c[0].Reason != (fold.BadSignature{}) {
		t.Errorf("after a tag change the forged event is %#v; want it still carried", c)
	}
	if again := fold.Genesis(env, fold.Missed{Miss: store.Absent{}}, store.NoSnapshot{}, w.scans()); again.Facts.Checked != n {
		t.Errorf("with the snapshot deleted Genesis checked %d; want %d", again.Facts.Checked, n)
	}
}

func TestAStreamUnderANewLogIsCheckedWhole(t *testing.T) {
	w, _ := signed(t)
	first := w.genesis()
	// B's stream is rewritten: the same records under a new log
	w.streams[b.ref()].log = store.LogID{0xee}
	got := fold.Genesis(env, fold.Missed{Miss: store.Rewritten{Ref: b.ref()}}, store.Saved{Tag: fold.Tag(), Marks: wrote(t, first).Marks, Payload: wrote(t, first).Payload}, w.scans())
	// B's four events, and the forged one as an exception
	if got.Facts.Checked != 5 || !got.State.Equal(first.State) {
		t.Errorf("a rewritten stream's genesis checked %d; want B's 4 and the 1 exception, and the same state", got.Facts.Checked)
	}
}

func TestAnInterleavedExtendChecksOnlyTheNewEventsAndCountsThemLate(t *testing.T) {
	w, id := item(t)
	for range 6 {
		w.mint(a, w.state(), set(id, title, prose("busy")))
	}
	first := w.genesis()
	snap, err := fold.ReadSnapshot(wrote(t, first).Payload)
	if err != nil {
		t.Fatal(err)
	}
	at := map[store.Ref]int{a.ref(): w.head(a.ref())}
	// B read only A's create, so its writes sort before what the snapshot holds
	stale := w.view(map[store.Ref]int{a.ref(): 1})
	for range 3 {
		w.mint(b, stale, addLabel(id, label("red")))
	}
	var delta []fold.Scanned
	for _, ref := range w.order {
		delta = append(delta, w.span(ref, at[ref], w.head(ref)))
	}
	got := fold.Extend(env, snap, wrote(t, first).Marks, delta)
	if got.Facts.Checked != 3 || got.Facts.Late != 3 || got.Facts.Base != (fold.Extended{}) {
		t.Errorf("an interleaved Extend checked %d, late %d, base %#v; want 3, 3, extended", got.Facts.Checked, got.Facts.Late, got.Facts.Base)
	}
	if !got.State.Equal(w.state()) {
		t.Errorf("an interleaved Extend disagrees with the genesis")
	}
}

func TestACleanHistorySavesNoExceptions(t *testing.T) {
	w, id := item(t)
	w.mint(b, w.state(), set(id, title, prose("clean")))
	got := w.genesis()
	snap, err := fold.ReadSnapshot(wrote(t, got).Payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Exceptions) != 0 || got.Facts.Exceptions != 0 {
		t.Errorf("a clean history saves exceptions %v (%d)", snap.Exceptions, got.Facts.Exceptions)
	}
}
