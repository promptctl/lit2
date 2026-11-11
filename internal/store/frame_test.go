package store

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

func mustBatch(t *testing.T, rs ...string) Batch {
	t.Helper()
	records := make([]Record, len(rs))
	for i, r := range rs {
		records[i] = Record(r)
	}
	b, err := NewBatch(records...)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func recordsOf(s Scan) []string {
	var rs []string
	for _, f := range s.Frames {
		for _, r := range f.Batch.Records() {
			rs = append(rs, string(r))
		}
	}
	return rs
}

// logOf is the stream of bs, one frame each, and every record in order.
func logOf(t *testing.T, bs ...Batch) ([]byte, []string) {
	t.Helper()
	var log []byte
	var rs []string
	for _, b := range bs {
		log = append(log, Encode(b)...)
		for _, r := range b.Records() {
			rs = append(rs, string(r))
		}
	}
	return log, rs
}

func TestABatchIsNeverEmptyNorOverAFrame(t *testing.T) {
	if _, err := NewBatch(); err == nil {
		t.Error("NewBatch() made an empty batch")
	}
	if _, err := NewBatch(make(Record, maxBody)); err == nil {
		t.Error("NewBatch made a batch over a frame's bound")
	}
	if b, err := NewBatch(Record(""), Record("x")); err != nil || len(b.Records()) != 2 {
		t.Errorf("NewBatch of an empty record and another = %v, %v", b, err)
	}
}

func TestEveryTruncationReadsAsWholeFramesAndAnIncompleteTail(t *testing.T) {
	log, _ := logOf(t, mustBatch(t, "a"), mustBatch(t, "bb", "ccc"), mustBatch(t, ""), mustBatch(t, "dddd"))
	ends := []Offset{}
	for _, f := range ScanFrames(0, log).Frames {
		ends = append(ends, f.At)
	}
	ends = append(ends[1:], Offset(len(log)))
	for cut := range len(log) + 1 {
		s := ScanFrames(0, log[:cut])
		whole := 0
		for whole < len(ends) && ends[whole] <= Offset(cut) {
			whole++
		}
		wantEnd := Offset(0)
		if whole > 0 {
			wantEnd = ends[whole-1]
		}
		if len(s.Frames) != whole || len(s.Torn) != 0 || s.End != wantEnd || s.Until != Offset(cut) {
			t.Errorf("cut at %d reads %d frames, %d torn, end %d, until %d; want %d frames, none torn, end %d", cut, len(s.Frames), len(s.Torn), s.End, s.Until, whole, wantEnd)
		}
	}
}

func TestGarbageBetweenFramesIsOneTornSpanAndLosesNothingElse(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for trial := range 200 {
		bs := make([]Batch, 1+rng.IntN(6))
		for i := range bs {
			bs[i] = mustBatch(t, fmt.Sprintf("record %d.%d", trial, i))
		}
		log, want := logOf(t, bs...)
		frames := ScanFrames(0, log).Frames
		at := frames[rng.IntN(len(frames))].At
		garbage := make([]byte, 1+rng.IntN(40))
		for i := range garbage {
			garbage[i] = byte(rng.Uint32())
		}
		if trial%3 == 0 { // the torn start of a real frame, as a crash leaves it
			f := Encode(mustBatch(t, "torn"))
			garbage = f[:1+rng.IntN(len(f)-1)]
		}
		torn := slices.Concat(log[:at], garbage, log[at:])
		s := ScanFrames(100, torn)
		if !slices.Equal(recordsOf(s), want) || len(s.Torn) != 1 || s.Torn[0] != (Span{From: 100 + at, To: 100 + at + Offset(len(garbage))}) || s.End != s.Until {
			t.Fatalf("trial %d: %d garbage bytes at %d read %v, torn %v; want every record and one torn span", trial, len(garbage), at, recordsOf(s), s.Torn)
		}
	}
}

func TestBytesThatAreNoFrameAreAnIncompleteTail(t *testing.T) {
	s := ScanFrames(7, []byte("no frame here"))
	if len(s.Frames) != 0 || len(s.Torn) != 0 || s.End != 7 || s.Until != 20 {
		t.Errorf("bytes with no frame read %+v; want nothing consumed", s)
	}
	f := Encode(mustBatch(t, "x"))
	f[len(f)-1] ^= 1
	if s := ScanFrames(0, f); len(s.Frames) != 0 || s.End != 0 {
		t.Errorf("a frame whose CRC disagrees read %+v; want no frame", s)
	}
	if !bytes.Equal(Encode(mustBatch(t, "x")), Encode(mustBatch(t, "x"))) {
		t.Error("one batch encodes two ways")
	}
}
