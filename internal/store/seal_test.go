package store

import (
	"bytes"
	"errors"
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

// kept is every segment of a publication as Kept, the open one last.
func kept(segs []Segment) []Kept {
	ks := make([]Kept, len(segs))
	for i, s := range segs {
		ks[i] = Kept{Span: span(s), Blob: git.ObjectID{}}
	}
	return ks
}

// A prior publication sealed under one bound, resumed under the same bound
// or a lower one, seals what sealing from scratch under the new bound does.
func TestSealingFromAPriorPublicationEqualsSealingFromScratch(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for trial := range 300 {
		log := randomLog(t, rng)
		was := SegmentBound(1 + rng.IntN(2000))
		bound := was
		if trial%2 == 1 {
			bound = SegmentBound(1 + rng.IntN(int(was)))
		}
		cut := rng.IntN(len(log) + 1)
		prior, from := keepWithin(kept(Seal(nil, Tail{From: 0, Bytes: log[:cut]}, was)), bound)
		incremental := Seal(prior, Tail{From: from, Bytes: log[from:]}, bound)
		scratch := Seal(nil, Tail{From: 0, Bytes: log}, bound)
		if fmt.Sprint(spans(incremental)) != fmt.Sprint(spans(scratch)) {
			t.Fatalf("trial %d, bound %d then %d, cut %d: incremental %v, scratch %v", trial, was, bound, cut, spans(incremental), spans(scratch))
		}
		// every non-empty random log starts with a whole frame, so it seals
		// to segments, and sealing from scratch stores the log's own bytes
		var sealed []byte
		for i, s := range scratch {
			f, ok := s.(Fresh)
			if !ok {
				t.Fatalf("trial %d: sealing from scratch made segment %d %#v; want it Fresh", trial, i, s)
			}
			sealed = append(sealed, f.Bytes...)
		}
		if !bytes.HasPrefix(log, sealed) {
			t.Fatalf("trial %d: a log of %d bytes sealed to segments holding %d other bytes; want a prefix of the log", trial, len(log), len(sealed))
		}
		// an empty log seals to nothing, which no publication holds; any
		// other seals to segments that tile it
		_, err := NewPublication(WriterRef{}, git.Absent{}, LogID{}, scratch)
		if len(log) == 0 && !errors.Is(err, errNoSegment) || len(log) > 0 && err != nil {
			t.Fatalf("trial %d: the %d segments sealed from %d bytes make a publication refused with %v", trial, len(scratch), len(log), err)
		}
		for i, s := range incremental {
			want := scratch[i].(Fresh).Bytes
			var got []byte
			switch s := s.(type) {
			case Kept:
				got = log[s.Span.From:s.Span.To]
			case Fresh:
				got = s.Bytes
			default:
				t.Fatalf("trial %d: segment %d is %T", trial, i, s)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("trial %d: segment %d holds other bytes when sealed from a prior publication", trial, i)
			}
		}
	}
}

// A raised bound keeps every full segment cut by the lower one; only new
// bytes are cut by it.
func TestARaisedBoundKeepsTheSmallerSegments(t *testing.T) {
	log := bytes.Repeat(Encode(mustBatch(t, "a record of some length")), 20)
	prior := kept(Seal(nil, Tail{From: 0, Bytes: log}, 100))
	full, from := keepWithin(prior, 1000)
	if len(full) != len(prior)-1 || from != prior[len(prior)-1].Span.From {
		t.Errorf("under a raised bound %d of %d segments are kept, resuming at %d; want every full one", len(full), len(prior), from)
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
	// the control: segments that tile, Kept then Fresh
	if _, err := NewPublication(InboxRef{}, git.Absent{}, LogID{}, []Segment{Kept{Span: Span{From: 0, To: 2}}, Fresh{Start: 2, Bytes: []byte("c")}}); err != nil {
		t.Fatalf("a tiling publication was refused: %v", err)
	}
	for name, c := range map[string]struct {
		segs []Segment
		want error
	}{
		"a gap":            {[]Segment{Fresh{Start: 0, Bytes: []byte("ab")}, Fresh{Start: 3, Bytes: []byte("c")}}, errMisplacedSegment},
		"an overlap":       {[]Segment{Kept{Span: Span{From: 0, To: 4}}, Fresh{Start: 2, Bytes: []byte("c")}}, errMisplacedSegment},
		"a late start":     {[]Segment{Fresh{Start: 1, Bytes: []byte("a")}}, errMisplacedSegment},
		"an empty segment": {[]Segment{Fresh{Start: 0, Bytes: []byte("a")}, Fresh{Start: 1, Bytes: nil}}, errEmptySegment},
		"no segment":       {[]Segment{}, errNoSegment},
	} {
		if _, err := NewPublication(InboxRef{}, git.Absent{}, LogID{}, c.segs); !errors.Is(err, c.want) {
			t.Errorf("%s: NewPublication(%v) = %v; want %v", name, c.segs, err, c.want)
		}
	}
}
