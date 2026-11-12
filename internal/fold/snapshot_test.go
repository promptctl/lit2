package fold_test

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/store"
)

func TestASnapshotIsDisposable(t *testing.T) {
	w := generate(t, 7, 30)
	want := w.genesis()
	if _, err := fold.ReadSnapshot([]byte("not a fold's payload")); err == nil {
		t.Fatal("ReadSnapshot read bytes no fold wrote")
	}
	torn := errors.New("the snapshot's checksum fails")
	for _, c := range []struct {
		why  store.Miss
		file store.SnapshotFile
	}{
		{store.Absent{}, store.NoSnapshot{}},
		{store.Torn{Why: torn}, store.TornSnapshot{Why: torn}},
	} {
		got := fold.Genesis(env, c.why, nil, c.file, w.scans())
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
	bumped := w.refold(store.Saved{Tag: store.FoldTag("lit2 fold 0"), Marks: wrote(t, first).Marks, Payload: wrote(t, first).Payload})
	if bumped.Facts.Checked != 1 || bumped.Facts.Trusted != n-1 {
		t.Errorf("after a tag change Genesis checked %d and trusted %d; want 1 and %d", bumped.Facts.Checked, bumped.Facts.Trusted, n-1)
	}
	if c := bumped.State.Integrity().Carried; len(c) != 1 || c[0].Event != forged.ID() || c[0].Reason != (fold.BadSignature{}) {
		t.Errorf("after a tag change the forged event is %#v; want it still carried", c)
	}
	if again := fold.Genesis(env, store.Absent{}, nil, store.NoSnapshot{}, w.scans()); again.Facts.Checked != n {
		t.Errorf("with the snapshot deleted Genesis checked %d; want %d", again.Facts.Checked, n)
	}
}

func TestAStreamUnderANewLogIsCheckedWhole(t *testing.T) {
	w, _ := signed(t)
	first := w.genesis()
	// B's stream is rewritten: the same records under a new log
	w.streams[b.ref()].log = store.LogID{0xee}
	got := w.refold(store.Saved{Tag: fold.Tag(), Marks: wrote(t, first).Marks, Payload: wrote(t, first).Payload})
	if got.Facts.Base != (fold.Refolded{Why: store.Rewritten{Ref: b.ref()}}) {
		t.Fatalf("the store plans %#v; want B's stream rewritten", got.Facts.Base)
	}
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

func TestACarriedEventMovesNeitherWhereTheSnapshotStopsNorWhatIsLate(t *testing.T) {
	w, id := item(t)
	for range 3 {
		w.mint(a, w.state(), set(id, title, prose("honest")))
	}
	first := w.genesis()
	snap, err := fold.ReadSnapshot(wrote(t, first).Payload)
	if err != nil {
		t.Fatal(err)
	}
	// a forger claims a position far past every honest writer's
	far := must(events.ReadFrontier(binary.AppendUvarint(nil, 1_000_000)))
	m, err := events.Mint(events.Context{Writer: checkoutB, Agent: events.Unlabeled{}, ConfigRef: configRef, Policy: 1, Frontier: far, Names: w.state().Names(), Now: t0},
		events.NewDrafts(set(id, title, prose("forged"))), forger{keyB.Signer()}, w.salts)
	if err != nil {
		t.Fatal(err)
	}
	w.append(b.ref(), m.Batch)
	carried := fold.Extend(env, snap, wrote(t, first).Marks, []fold.Scanned{w.span(b.ref(), 0, 1)})
	after, err := fold.ReadSnapshot(wrote(t, carried).Payload)
	if err != nil {
		t.Fatal(err)
	}
	if after.Through != snap.Through || carried.Facts.Late != 0 || len(carried.State.Integrity().Carried) != 1 {
		t.Errorf("a carried far event moves the snapshot to %#v with %d late; want it where it was, none late", after.Through, carried.Facts.Late)
	}
	// an honest write after it is not late
	w.mint(a, w.state(), set(id, title, prose("later")))
	next := fold.Extend(env, after, wrote(t, carried).Marks, []fold.Scanned{w.span(a.ref(), w.head(a.ref())-1, w.head(a.ref()))})
	if next.Facts.Late != 0 || next.Facts.Checked != 1 {
		t.Errorf("an honest write after a carried far one is %d late of %d checked; want 0 of 1", next.Facts.Late, next.Facts.Checked)
	}
}

func TestAGenesisAfterAStreamShortensChecksThatStreamWhole(t *testing.T) {
	w, _ := signed(t)
	first := w.genesis()
	// B's stream loses its last batch under the same log
	bs := w.streams[b.ref()]
	bs.batches = bs.batches[:len(bs.batches)-1]
	got := w.refold(store.Saved{Tag: fold.Tag(), Marks: wrote(t, first).Marks, Payload: wrote(t, first).Payload})
	if got.Facts.Base != (fold.Refolded{Why: store.Rewritten{Ref: b.ref()}}) {
		t.Fatalf("a shortened stream plans %#v; want it rewritten", got.Facts.Base)
	}
	// B's 3 remaining events checked, A's 5 trusted but its forged one,
	// which is an exception and checked again
	if got.Facts.Checked != 4 || got.Facts.Trusted != 5 {
		t.Errorf("a genesis after B shortened checked %d and trusted %d; want 4 and 5", got.Facts.Checked, got.Facts.Trusted)
	}
}

func TestASnapshotRecordMissingItsKeyDoesNotRead(t *testing.T) {
	w, _, _, _ := base(t)
	payload := wrote(t, w.genesis()).Payload
	// the payload rewritten with nothing dropped reads as it was, so the
	// rewrite is not what a refusal below refuses
	same, _ := without(t, payload, 0, 2, 4, 5)
	if snap, err := fold.ReadSnapshot(same); err != nil || !snap.State.Equal(w.state()) {
		t.Fatalf("a rewritten payload reads %v", err)
	}
	for _, c := range []struct {
		name string
		drop uint32
		path []uint32
	}{
		{"order key", 1, []uint32{2, 5}},
		{"ref", 1, []uint32{2, 7}},
		{"digest", 3, []uint32{2, 7}},
		{"name", 1, []uint32{2, 4, 5}},
		{"kind name", 2, []uint32{2, 4, 5, 2}},
		{"field name", 1, []uint32{2, 4, 4, 1}},
		{"set name", 1, []uint32{2, 4, 4, 2}},
		{"member name", 1, []uint32{2, 4, 4, 2, 2}},
		{"placement", 2, []uint32{2, 4, 2}},
	} {
		dropped, ok := without(t, payload, c.drop, c.path...)
		if !ok {
			t.Fatalf("the base world's payload has no field %d at %v", c.drop, c.path)
		}
		if _, err := fold.ReadSnapshot(dropped); err == nil || !strings.Contains(err.Error(), c.name) {
			t.Errorf("a payload whose record has no %s reads, or is refused for %v", c.name, err)
		}
	}
}

// without is b with field drop removed from the first message at path
// holding it, and whether one held it. Every message on path is rewritten
// with its other fields first, in stored order, so the attribution table
// still precedes every stamp.
func without(t *testing.T, b []byte, drop uint32, path ...uint32) ([]byte, bool) {
	t.Helper()
	r := wire.NewReader(b)
	type sub struct {
		tag uint32
		b   []byte
	}
	var subs []sub
	done := false
	for tag, ok := r.Next(); ok; tag, ok = r.Next() {
		switch {
		case done:
		case len(path) == 0 && tag == drop:
			r.Bytes()
			done = true
		case len(path) > 0 && tag == path[0]:
			inner, d := without(t, r.Bytes(), drop, path[1:]...)
			subs, done = append(subs, sub{tag: tag, b: inner}), d
		}
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	var out wire.Writer
	out.Keep(r.Rest())
	for _, s := range subs {
		out.Bytes(s.tag, s.b)
	}
	return out.Encoded(), done
}
