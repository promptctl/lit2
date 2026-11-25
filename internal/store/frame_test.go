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
		var wantTail ScanTail = NoTail{}
		if wantEnd < Offset(cut) {
			wantTail = InFlight{Span: Span{From: wantEnd, To: Offset(cut)}}
		}
		if len(s.Frames) != whole || len(s.Torn) != 0 || s.End != wantEnd || s.Tail != wantTail || s.Until() != Offset(cut) {
			t.Errorf("cut at %d reads %d frames, %d torn, end %d, tail %#v; want %d frames, none torn, end %d, tail %#v", cut, len(s.Frames), len(s.Torn), s.End, s.Tail, whole, wantEnd, wantTail)
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
		if !slices.Equal(recordsOf(s), want) || len(s.Torn) != 1 || s.Torn[0].Span != (Span{From: 100 + at, To: 100 + at + Offset(len(garbage))}) || s.Tail != (NoTail{}) {
			t.Fatalf("trial %d: %d garbage bytes at %d read %v, torn %v; want every record and one torn span", trial, len(garbage), at, recordsOf(s), s.Torn)
		}
	}
}

func TestBytesThatAreNoFrameAreATailNothingConsumes(t *testing.T) {
	s := ScanFrames(7, []byte("no frame here"))
	if len(s.Frames) != 0 || len(s.Torn) != 0 || s.End != 7 || s.Until() != 20 {
		t.Errorf("bytes with no frame read %+v; want nothing consumed", s)
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
	refused := []struct {
		name  string
		bytes []byte
		why   Refusal
	}{
		{"no magic", []byte("no frame here"), NoMagic{}},
		{"a body cut short", framed(64, []byte("short")), Truncated{}},
		{"a length over the bound", framed(maxBody+1, nil), OverBound{Length: maxBody + 1}},
		{"a CRC that disagrees", crc, CRCMismatch{}},
		{"a record longer than its body", framed(6, []byte{0, 0, 0, 9, 'a', 'b'}), BadRecordLength{}},
		{"a body shorter than a record header", framed(2, []byte{0, 0}), BadRecordLength{}},
		{"a CRC that holds over no records", framed(0, nil), NoRecords{}},
	}
	for _, r := range refused {
		at := Offset(len(good))
		span := Span{From: at, To: at + Offset(len(r.bytes))}
		mid := ScanFrames(0, slices.Concat(good, r.bytes, good))
		if len(mid.Frames) != 2 || !slices.Equal(mid.Torn, []Torn{{Span: span, Why: r.why}}) || mid.Tail != (NoTail{}) {
			t.Errorf("%s between two frames read %d frames, torn %#v, tail %#v; want 2 frames, torn %#v", r.name, len(mid.Frames), mid.Torn, mid.Tail, Torn{Span: span, Why: r.why})
		}
		// at the tail only bytes more bytes could finish are an append in
		// flight; the rest are torn whatever follows them
		var want ScanTail = InFlight{Span: span}
		if m, malformed := r.why.(Malformed); malformed {
			want = TornTail{Span: span, Why: m}
		}
		tail := ScanFrames(0, slices.Concat(good, r.bytes))
		if len(tail.Frames) != 1 || len(tail.Torn) != 0 || tail.End != at || tail.Tail != want {
			t.Errorf("%s at the tail read %d frames, torn %#v, end %d, tail %#v; want 1 frame, end %d, tail %#v", r.name, len(tail.Frames), tail.Torn, tail.End, tail.Tail, at, want)
		}
	}
}
