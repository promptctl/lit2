package store

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/promptctl/lit2/internal/git"
)

// randomLog is a stream of frames of random sizes, with torn bytes between
// some of them.
func randomLog(t *testing.T, rng *rand.Rand) []byte {
	t.Helper()
	var log []byte
	for i := range rng.IntN(60) {
		rs := make([]Record, 1+rng.IntN(3))
		for j := range rs {
			rs[j] = Record(bytes.Repeat([]byte{byte('a' + j)}, rng.IntN(200)))
		}
		b, err := NewBatch(rs...)
		if err != nil {
			t.Fatal(err)
		}
		log = append(log, Encode(b)...)
		if i%7 == 3 {
			log = append(log, "torn bytes"[:1+rng.IntN(9)]...)
		}
	}
	return log
}

func spans(segs []Segment) []Span {
	ss := make([]Span, len(segs))
	for i, s := range segs {
		ss[i] = span(s)
	}
	return ss
}

func TestSealingFromAPriorPublicationEqualsSealingFromScratch(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for trial := range 300 {
		log := randomLog(t, rng)
		bound := SegmentBound(1 + rng.IntN(2000))
		cut := rng.IntN(len(log) + 1)
		first := Seal(nil, Tail{From: 0, Bytes: log[:cut]}, bound)
		var prior []Kept
		from := Offset(0)
		if len(first) > 0 {
			for _, s := range first[:len(first)-1] {
				prior = append(prior, Kept{Span: span(s), Blob: git.ObjectID{}})
			}
			from = span(first[len(first)-1]).From
		}
		incremental := Seal(prior, Tail{From: from, Bytes: log[from:]}, bound)
		scratch := Seal(nil, Tail{From: 0, Bytes: log}, bound)
		if fmt.Sprint(spans(incremental)) != fmt.Sprint(spans(scratch)) {
			t.Fatalf("trial %d, bound %d, cut %d: incremental %v, scratch %v", trial, bound, cut, spans(incremental), spans(scratch))
		}
		for i, s := range scratch {
			if f, ok := incremental[i].(Fresh); ok && !bytes.Equal(f.Bytes, s.(Fresh).Bytes) {
				t.Fatalf("trial %d: segment %d holds other bytes when sealed from a prior publication", trial, i)
			}
		}
		if _, err := NewPublication(WriterRef{}, LogID{}, scratch); err != nil {
			t.Fatalf("trial %d: the sealed segments do not tile the stream: %v", trial, err)
		}
	}
}

func TestASegmentStaysWithinItsBoundUnlessOneFrameIsLarger(t *testing.T) {
	big, err := NewBatch(make(Record, 500))
	if err != nil {
		t.Fatal(err)
	}
	small := Encode(mustBatch(t, "s"))
	log := append(append(append([]byte{}, small...), Encode(big)...), small...)
	got := spans(Seal(nil, Tail{From: 0, Bytes: log}, 100))
	s, b := Offset(len(small)), Offset(len(Encode(big)))
	want := []Span{{0, s}, {s, s + b}, {s + b, 2*s + b}}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("sealing small, big, small under 100 bytes cut %v; want %v", got, want)
	}
}

func TestAPublicationHasNoGapAndNoOverlap(t *testing.T) {
	for name, segs := range map[string][]Segment{
		"a gap":            {Fresh{Start: 0, Bytes: []byte("ab")}, Fresh{Start: 3, Bytes: []byte("c")}},
		"an overlap":       {Kept{Span: Span{From: 0, To: 4}}, Fresh{Start: 2, Bytes: []byte("c")}},
		"a late start":     {Fresh{Start: 1, Bytes: []byte("a")}},
		"an empty segment": {Fresh{Start: 0, Bytes: nil}},
	} {
		if _, err := NewPublication(InboxRef{}, LogID{}, segs); err == nil {
			t.Errorf("%s: NewPublication accepted %v", name, segs)
		}
	}
	if _, err := NewPublication(InboxRef{}, LogID{}, []Segment{Kept{Span: Span{From: 0, To: 2}}, Fresh{Start: 2, Bytes: []byte("c")}}); err != nil {
		t.Errorf("a tiling publication was refused: %v", err)
	}
}
