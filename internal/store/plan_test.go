package store

import (
	"errors"
	"reflect"
	"testing"

	"github.com/promptctl/lit2/internal/git"
)

var (
	w1, w2 = WriterRef{Checkout: CheckoutID{1}}, WriterRef{Checkout: CheckoutID{2}}
	inbox  = InboxRef{Inbox: InboxID{3}}
	logA   = LogID{0xa}
	logB   = LogID{0xb}
	v1     = FoldTag("v1")
)

func headsOf(streams ...Stream) Heads { return Heads{Streams: streams} }

func at(ref Ref, log LogID, until Offset) Stream {
	return Stream{Ref: ref, Until: Position{Log: log, At: until}}
}

func saved(marks ...Mark) Saved { return Saved{Tag: v1, Marks: marks, Payload: []byte("state")} }

func mark(ref Ref, log LogID, upto Offset) Mark {
	return Mark{Ref: ref, Upto: Position{Log: log, At: upto}}
}

func TestPlanResumesExtendsOrRefolds(t *testing.T) {
	now := headsOf(at(w1, logA, 100), at(w2, logB, 50), at(inbox, logB, 10))
	gone := WriterRef{Checkout: CheckoutID{9}}
	for _, tc := range []struct {
		name  string
		file  SnapshotFile
		base  Base
		held  []Mark
		delta map[Ref]Offset
	}{
		{"no snapshot is genesis, every stream from 0", NoSnapshot{}, Genesis{Why: Absent{}}, []Mark{}, map[Ref]Offset{w1: 0, w2: 0, inbox: 0}},
		{"a torn snapshot is genesis", TornSnapshot{Why: errors.New("crc")}, Genesis{Why: Torn{Why: errors.New("crc")}}, []Mark{}, map[Ref]Offset{w1: 0, w2: 0, inbox: 0}},
		{"another fold version is genesis, keeping the marks that hold",
			Saved{Tag: FoldTag("v0"), Marks: []Mark{mark(w1, logA, 60), mark(w2, logB, 70)}}, Genesis{Why: TagChanged{}}, []Mark{mark(w1, logA, 60)}, map[Ref]Offset{w1: 0, w2: 0, inbox: 0}},
		{"a snapshot at every head is a hit",
			saved(mark(w1, logA, 100), mark(w2, logB, 50), mark(inbox, logB, 10)), FromSaved{Payload: []byte("state")},
			[]Mark{mark(w1, logA, 100), mark(w2, logB, 50), mark(inbox, logB, 10)}, map[Ref]Offset{}},
		{"grown and new streams are read from where the snapshot stopped",
			saved(mark(w1, logA, 60), mark(w2, logB, 50)), FromSaved{Payload: []byte("state")}, []Mark{mark(w1, logA, 60), mark(w2, logB, 50)}, map[Ref]Offset{w1: 60, inbox: 0}},
		{"a stream of another log was rewritten",
			saved(mark(w1, logB, 60), mark(w2, logB, 50)), Genesis{Why: Rewritten{Ref: w1}}, []Mark{mark(w2, logB, 50)}, map[Ref]Offset{w1: 0, w2: 0, inbox: 0}},
		{"a stream shorter than the snapshot read was rewritten",
			saved(mark(w2, logB, 70), mark(inbox, logB, 10)), Genesis{Why: Rewritten{Ref: w2}}, []Mark{mark(inbox, logB, 10)}, map[Ref]Offset{w1: 0, w2: 0, inbox: 0}},
		{"a stream that is gone was withdrawn",
			saved(mark(gone, logA, 5), mark(w1, logA, 100)), Genesis{Why: Withdrawn{Ref: gone}}, []Mark{mark(w1, logA, 100)}, map[Ref]Offset{w1: 0, w2: 0, inbox: 0}},
		{"every mark is judged, the first miss named",
			saved(mark(w1, logB, 60), mark(gone, logA, 5), mark(inbox, logB, 10)), Genesis{Why: Rewritten{Ref: w1}}, []Mark{mark(inbox, logB, 10)}, map[Ref]Offset{w1: 0, w2: 0, inbox: 0}},
	} {
		r := Plan(v1, now, tc.file)
		delta := map[Ref]Offset{}
		for _, d := range r.Delta {
			delta[d.Stream.Ref] = d.From
		}
		if !reflect.DeepEqual(r.Base, tc.base) || !reflect.DeepEqual(delta, tc.delta) || !reflect.DeepEqual(r.Held, tc.held) {
			t.Errorf("%s: Plan = %#v holding %v from %v; want %#v holding %v from %v", tc.name, r.Base, r.Held, delta, tc.base, tc.held, tc.delta)
		}
	}
}

func TestASnapshotFileReadsBackOrIsTorn(t *testing.T) {
	marks := []Mark{mark(w1, logA, 7), mark(inbox, logB, 1<<40)}
	b := encodeSnapshot(v1, marks, []byte("payload"))
	if got := decodeSnapshot(b); !reflect.DeepEqual(got, Saved{Tag: v1, Marks: marks, Payload: []byte("payload")}) {
		t.Errorf("a snapshot reads back as %#v", got)
	}
	for cut := range len(b) {
		if _, torn := decodeSnapshot(b[:cut]).(TornSnapshot); !torn {
			t.Errorf("a snapshot cut at %d of %d bytes is not torn", cut, len(b))
		}
	}
	twice := encodeSnapshot(v1, []Mark{marks[0], marks[0]}, nil)
	if _, torn := decodeSnapshot(twice).(TornSnapshot); !torn {
		t.Error("a snapshot marking one stream twice is not torn")
	}
}

func TestARefNameParsesBackOrIsRefused(t *testing.T) {
	for _, r := range []Ref{w1, inbox, WriterRef{Checkout: CheckoutID{0xff, 0x01}}} {
		if back, err := ParseRef(r.Name()); err != nil || back != r {
			t.Errorf("ParseRef(%s) = %v, %v; want it back", r.Name(), back, err)
		}
	}
	for _, n := range []git.RefName{
		"refs/lit/log/", "refs/lit/log/0A000000000000000000000000000000", "refs/lit/log/0a00000000000000000000000000000",
		"refs/lit/inbox/0a000000000000000000000000000000/x", "refs/lit/other/0a000000000000000000000000000000", "refs/heads/main",
	} {
		if r, err := ParseRef(n); err == nil {
			t.Errorf("ParseRef(%s) read %v", n, r)
		}
	}
}
