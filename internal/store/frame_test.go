package store

import (
	"bytes"
	"encoding/binary"
	"errors"
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
	// the control: one record of the most bytes a frame carries with its
	// header, and a batch of an empty record and another
	if _, err := NewBatch(make(Record, maxBody-recordHeader)); err != nil {
		t.Fatalf("NewBatch of a frame's bound refused: %v", err)
	}
	if b, err := NewBatch(Record(""), Record("x")); err != nil || len(b.Records()) != 2 {
		t.Errorf("NewBatch of an empty record and another = %v, %v", b, err)
	}
	if _, err := NewBatch(); !errors.Is(err, errEmptyBatch) {
		t.Errorf("NewBatch() = %v; want errEmptyBatch", err)
	}
	if _, err := NewBatch(make(Record, maxBody-recordHeader+1)); !errors.Is(err, errOverAFrame) {
		t.Errorf("NewBatch of one byte over a frame's bound = %v; want errOverAFrame", err)
	}
}

func TestEveryTruncationReadsAsWholeFramesAndAnIncompleteTail(t *testing.T) {
	bs := []Batch{mustBatch(t, "a"), mustBatch(t, "bb", "ccc"), mustBatch(t, ""), mustBatch(t, "dddd")}
	log, _ := logOf(t, bs...)
	// each frame's end, from the encoded lengths alone
	ends := make([]Offset, len(bs))
	var at Offset
	for i, b := range bs {
		at += Offset(len(Encode(b)))
		ends[i] = at
	}
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
		inFlight := Span{From: wantEnd, To: Offset(cut)}
		if len(s.Frames) != whole || len(s.Torn) != 0 || s.End != wantEnd || s.InFlight() != inFlight {
			t.Errorf("cut at %d reads %d frames, %d torn, end %d, in flight %v; want %d frames, none torn, end %d, in flight %v", cut, len(s.Frames), len(s.Torn), s.End, s.InFlight(), whole, wantEnd, inFlight)
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
		if !slices.Equal(recordsOf(s), want) || len(s.Torn) != 1 || s.Torn[0].Span != (Span{From: 100 + at, To: 100 + at + Offset(len(garbage))}) || s.InFlight().Len() != 0 {
			t.Fatalf("trial %d: %d garbage bytes at %d read %v, torn %v; want every record and one torn span", trial, len(garbage), at, recordsOf(s), s.Torn)
		}
	}
}

func TestBytesThatAreNoFrameAreTornAndNothingConsumes(t *testing.T) {
	s := ScanFrames(7, []byte("no frame here"))
	if len(s.Frames) != 0 || !slices.Equal(s.Torn, []Torn{{Span: Span{From: 7, To: 20}, Why: NoMagic{}}}) || s.End != 7 || s.InFlight() != (Span{From: 20, To: 20}) {
		t.Errorf("bytes with no frame read %+v; want them torn, nothing consumed, nothing in flight", s)
	}
	if !bytes.Equal(Encode(mustBatch(t, "x")), Encode(mustBatch(t, "x"))) {
		t.Error("one batch encodes two ways")
	}
}

// framed is a frame of length and body under a CRC that holds for them.
func framed(length uint32, body []byte) []byte {
	f := append([]byte{}, frameMagic[:]...)
	f = binary.BigEndian.AppendUint32(f, length)
	f = binary.BigEndian.AppendUint32(f, frameCRC(f[4:8], body))
	return append(f, body...)
}

func TestEachRefusalSaysWhyItsBytesAreNoFrame(t *testing.T) {
	good := Encode(mustBatch(t, "good"))
	crc := Encode(mustBatch(t, "x"))
	crc[len(crc)-1] ^= 1
	// at the tail only bytes more bytes could finish are in flight; the
	// rest are torn whatever follows them
	refused := []struct {
		name     string
		bytes    []byte
		why      Refusal
		inFlight bool
	}{
		{"no magic", []byte("no frame here"), NoMagic{}, false},
		{"a body cut short", framed(64, []byte("short")), Truncated{}, true},
		{"a length over the bound", framed(maxBody+1, nil), OverBound{Length: maxBody + 1}, false},
		{"a CRC that disagrees", crc, CRCMismatch{}, false},
		{"a record longer than its body", framed(6, []byte{0, 0, 0, 9, 'a', 'b'}), BadRecordLength{}, false},
		{"a body shorter than a record header", framed(2, []byte{0, 0}), BadRecordLength{}, false},
		{"a CRC that holds over no records", framed(0, nil), NoRecords{}, false},
	}
	for _, r := range refused {
		at := Offset(len(good))
		span := Span{From: at, To: at + Offset(len(r.bytes))}
		mid := ScanFrames(0, slices.Concat(good, r.bytes, good))
		if len(mid.Frames) != 2 || !slices.Equal(mid.Torn, []Torn{{Span: span, Why: r.why}}) || mid.InFlight().Len() != 0 {
			t.Errorf("%s between two frames read %d frames, torn %#v, in flight %v; want 2 frames, torn %#v", r.name, len(mid.Frames), mid.Torn, mid.InFlight(), Torn{Span: span, Why: r.why})
		}
		torn, inFlight := []Torn{{Span: span, Why: r.why}}, Span{From: span.To, To: span.To}
		if r.inFlight {
			torn, inFlight = []Torn{}, span
		}
		tail := ScanFrames(0, slices.Concat(good, r.bytes))
		if len(tail.Frames) != 1 || !slices.Equal(tail.Torn, torn) || tail.End != at || tail.InFlight() != inFlight {
			t.Errorf("%s at the tail read %d frames, torn %#v, end %d, in flight %v; want 1 frame, torn %#v, end %d, in flight %v", r.name, len(tail.Frames), tail.Torn, tail.End, tail.InFlight(), torn, at, inFlight)
		}
	}
}

// Torn bytes after the last whole frame and an append in flight after them
// are each what they are: the append starts at the first frame the bytes
// cut short, even one whose body holds the magic, or at a start of the
// magic the bytes end on.
func TestTornBytesBeforeAnAppendInFlightAreTornAndTheAppendIsInFlight(t *testing.T) {
	good := Encode(mustBatch(t, "good"))
	crc := Encode(mustBatch(t, "x"))
	crc[len(crc)-1] ^= 1
	inner := Encode(mustBatch(t, "holds "+string(frameMagic[:])+" in its body"))
	inner = inner[:len(inner)-4] // cut after the magic in its body
	at := Offset(len(good))
	for _, tc := range []struct {
		name     string
		after    []byte
		torn     []Torn
		inFlight Span
	}{
		{"zeros, then a frame's first bytes", slices.Concat(make([]byte, 8), Encode(mustBatch(t, "landing"))[:6]),
			[]Torn{{Span: Span{From: at, To: at + 8}, Why: NoMagic{}}}, Span{From: at + 8, To: at + 14}},
		{"a CRC that disagrees, then a frame's first bytes", slices.Concat(crc, Encode(mustBatch(t, "landing"))[:10]),
			[]Torn{{Span: Span{From: at, To: at + Offset(len(crc))}, Why: CRCMismatch{}}}, Span{From: at + Offset(len(crc)), To: at + Offset(len(crc)) + 10}},
		{"a frame cut short whose body holds the magic", inner,
			[]Torn{}, Span{From: at, To: at + Offset(len(inner))}},
		{"zeros, then a start of the magic", slices.Concat([]byte("zz"), frameMagic[:2]),
			[]Torn{{Span: Span{From: at, To: at + 2}, Why: NoMagic{}}}, Span{From: at + 2, To: at + 4}},
	} {
		s := ScanFrames(0, slices.Concat(good, tc.after))
		if len(s.Frames) != 1 || s.End != at || !slices.Equal(s.Torn, tc.torn) || s.InFlight() != tc.inFlight {
			t.Errorf("%s after a frame read %d frames, end %d, torn %#v, in flight %v; want 1 frame, end %d, torn %#v, in flight %v", tc.name, len(s.Frames), s.End, s.Torn, s.InFlight(), at, tc.torn, tc.inFlight)
		}
	}
}
