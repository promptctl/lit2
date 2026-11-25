package store

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
)

// Offset is a byte offset in a writer's stream. A writer's segments
// concatenate to a prefix of its log, so an offset means the same in both.
type Offset uint64

// Span is the bytes [From, To) of a stream.
type Span struct{ From, To Offset }

// Len is the span's length in bytes.
func (s Span) Len() uint64 { return uint64(s.To - s.From) }

// LogID is the identity of one log; a rewritten log is a new LogID, so an
// offset into it never compares with one into the old.
type LogID [16]byte

// String writes the id as 32 lowercase hex digits.
func (id LogID) String() string { return hex.EncodeToString(id[:]) }

// Position is a place in one log or publication: the log and the offset.
type Position struct {
	Log LogID
	At  Offset
}

// Record is one event as the events codec wrote it, opaque here: the codec
// owns what is inside a frame, the store where frames start and end.
type Record []byte

// Batch is one command's records, all of them, so a command's append is
// whole or absent. It cannot be empty.
type Batch struct {
	first Record
	rest  []Record
}

// maxBody bounds one frame's body, so a length read from torn bytes cannot
// ask a reader for more than a command could have written.
const maxBody = 1 << 24

// The two batches NewBatch refuses.
var (
	errEmptyBatch = errors.New("a batch of no record")
	errOverAFrame = errors.New("a batch over what one frame carries")
)

// NewBatch is the batch of rs, refusing none and refusing more bytes than
// one frame carries.
func NewBatch(rs ...Record) (Batch, error) {
	if len(rs) == 0 {
		return Batch{}, errEmptyBatch
	}
	b := Batch{first: rs[0], rest: rs[1:]}
	if n := b.bodySize(); n > maxBody {
		return Batch{}, fmt.Errorf("a batch of %d bytes, over the %d one frame carries: %w", n, maxBody, errOverAFrame)
	}
	return b, nil
}

// Records is the batch's records in order.
func (b Batch) Records() []Record { return append([]Record{b.first}, b.rest...) }

func (b Batch) bodySize() int {
	n := 0
	for _, r := range b.Records() {
		n += recordHeader + len(r)
	}
	return n
}

// Frame is one append, a command's whole batch, at its offset in the stream.
type Frame struct {
	At    Offset
	Batch Batch
}

// Scan is what a reader finds in a stream's bytes, up to Until: whole
// frames; torn spans, bytes no whole frame holds and no more bytes can make
// one of, skipped and counted, each with why the frame at its start was
// refused; and End, the offset after the last whole frame. Bytes after End
// are never consumed and never truncated (the store's decision 4): torn
// bytes there become a torn span between frames once a frame follows them,
// and InFlight is the rest.
type Scan struct {
	Frames []Frame
	Torn   []Torn
	End    Offset
	Until  Offset
}

// InFlight is the bytes after the last whole frame, and after the torn
// spans that follow it, that more bytes could make a frame of: an append in
// flight, or one a crash cut short, which look the same. It is empty when
// there are none.
func (s Scan) InFlight() Span {
	from := s.End
	if n := len(s.Torn); n > 0 && s.Torn[n-1].To > from {
		from = s.Torn[n-1].To
	}
	return Span{From: from, To: s.Until}
}

// Torn is a torn span, and why the frame at its start was refused; magic
// later in the span is only bytes.
type Torn struct {
	Span
	Why Refusal
}

// Refusal is why the bytes at a place are not a whole frame, as the
// frame's own bytes say it, and Finishable, whether more bytes after them
// could make it one: only Truncated, whose bytes stop before its length
// does. So an append a crash cut short reads as Truncated while nothing
// follows it, as an append in flight does, and once a later append follows
// it, as a CRCMismatch, its length then covering bytes it never wrote, or
// as Truncated still when its length runs past every byte there.
//
//sumtype:decl
type Refusal interface {
	refusal()
	Finishable() bool
}

// Truncated is a frame whose header or body runs past the bytes there.
type Truncated struct{}

// NoMagic is bytes that do not start with the frame's magic.
type NoMagic struct{}

// OverBound is a frame whose length is over the body's bound.
type OverBound struct{ Length uint32 }

// CRCMismatch is a frame whose CRC disagrees with its length and body.
type CRCMismatch struct{}

// BadRecordLength is a frame whose body does not divide into its records.
type BadRecordLength struct{}

// NoRecords is a frame whose CRC holds and whose body has no records.
type NoRecords struct{}

func (Truncated) refusal()       {}
func (NoMagic) refusal()         {}
func (OverBound) refusal()       {}
func (CRCMismatch) refusal()     {}
func (BadRecordLength) refusal() {}
func (NoRecords) refusal()       {}

func (Truncated) Finishable() bool       { return true }
func (NoMagic) Finishable() bool         { return false }
func (OverBound) Finishable() bool       { return false }
func (CRCMismatch) Finishable() bool     { return false }
func (BadRecordLength) Finishable() bool { return false }
func (NoRecords) Finishable() bool       { return false }

// read is what the bytes at a place are: a whole frame, its batch and its
// length; or no frame, and why.
//
//sumtype:decl
type read interface{ read() }

type (
	wholeFrame struct {
		batch Batch
		n     int
	}
	noFrame struct{ why Refusal }
)

func (wholeFrame) read() {}
func (noFrame) read()    {}

// The frame: 4-byte magic, 4-byte body length, CRC-32C over the length and
// the body, then the body, its records each length-prefixed.
const (
	frameHeader  = 12
	recordHeader = 4
)

var (
	frameMagic = [4]byte{0xf1, 'l', 'i', 't'}
	castagnoli = crc32.MakeTable(crc32.Castagnoli)
)

// Encode is the batch as one frame, written by one append.
func Encode(b Batch) []byte {
	body := b.bodySize()
	f := make([]byte, frameHeader, frameHeader+body)
	copy(f, frameMagic[:])
	binary.BigEndian.PutUint32(f[4:], uint32(body))
	for _, r := range b.Records() {
		f = binary.BigEndian.AppendUint32(f, uint32(len(r)))
		f = append(f, r...)
	}
	binary.BigEndian.PutUint32(f[8:], frameCRC(f[4:8], f[frameHeader:]))
	return f
}

func frameCRC(length, body []byte) uint32 {
	return crc32.Update(crc32.Checksum(length, castagnoli), castagnoli, body)
}

// ScanFrames reads b, the stream's bytes from offset from. Where no whole
// frame starts it resynchronises on the next magic, so the bytes between
// two whole frames are one torn span and nothing on either side is lost.
func ScanFrames(from Offset, b []byte) Scan {
	at := func(i int) Offset { return from + Offset(i) }
	s := Scan{Frames: []Frame{}, Torn: []Torn{}, End: from, Until: at(len(b))}
	for pos, r := 0, frameAt(b); pos < len(b); {
		switch f := r.(type) {
		case wholeFrame:
			s.Frames = append(s.Frames, Frame{At: at(pos), Batch: f.batch})
			pos += f.n
			s.End = at(pos)
			r = frameAt(b[pos:])
		case noFrame:
			next, cut, following := unframed(b, pos, f.why)
			if cut > pos {
				s.Torn = append(s.Torn, Torn{Span: Span{From: at(pos), To: at(cut)}, Why: f.why})
			}
			pos, r = next, following
		}
	}
	return s
}

// unframed is where the bytes from pos, whose frame was refused for why,
// stop being no frame: next, the next whole frame's start, read as
// following, or the bytes' end, where following is no frame; and cut,
// where its torn bytes stop. That is next, unless no whole frame follows
// and a finishable frame does: then it is the first such frame, since
// every frame after it lies in the bytes its length claims.
func unframed(b []byte, pos int, why Refusal) (next, cut int, following read) {
	cut, finishable := len(b), why.Finishable()
	if finishable {
		cut = pos
	}
	for q := nextMagic(b, pos); q < len(b); q = nextMagic(b, q) {
		switch f := frameAt(b[q:]).(type) {
		case wholeFrame:
			return q, q, f
		case noFrame:
			if !finishable && f.why.Finishable() {
				cut, finishable = q, true
			}
		}
	}
	return len(b), cut, noFrame{why: Truncated{}}
}

// nextMagic is where after q the next frame may start: the next place the
// bytes start with the magic, or with a start of it they end on, or the
// bytes' end.
func nextMagic(b []byte, q int) int {
	if i := bytes.Index(b[q+1:], frameMagic[:]); i >= 0 {
		return q + 1 + i
	}
	for r := max(q+1, len(b)-len(frameMagic)+1); r < len(b); r++ {
		if magicAt(b[r:]) {
			return r
		}
	}
	return len(b)
}

// magicAt is whether b starts with the magic, or, shorter than it, with a
// start of it.
func magicAt(b []byte) bool {
	return bytes.HasPrefix(b, frameMagic[:min(len(b), len(frameMagic))])
}

// frameAt reads the whole frame b starts with, or why b starts with none.
func frameAt(b []byte) read {
	if !magicAt(b) {
		return noFrame{why: NoMagic{}}
	}
	if len(b) < 8 {
		return noFrame{why: Truncated{}}
	}
	length := binary.BigEndian.Uint32(b[4:])
	if length > maxBody {
		return noFrame{why: OverBound{Length: length}}
	}
	if frameHeader+int(length) > len(b) {
		return noFrame{why: Truncated{}}
	}
	body := b[frameHeader : frameHeader+int(length)]
	if frameCRC(b[4:8], body) != binary.BigEndian.Uint32(b[8:]) {
		return noFrame{why: CRCMismatch{}}
	}
	var rs []Record
	for len(body) > 0 {
		if len(body) < recordHeader {
			return noFrame{why: BadRecordLength{}}
		}
		n := binary.BigEndian.Uint32(body)
		if uint64(n) > uint64(len(body)-recordHeader) {
			return noFrame{why: BadRecordLength{}}
		}
		rs = append(rs, Record(body[recordHeader:recordHeader+int(n)]))
		body = body[recordHeader+int(n):]
	}
	batch, err := NewBatch(rs...)
	switch {
	case errors.Is(err, errEmptyBatch):
		return noFrame{why: NoRecords{}}
	case err != nil:
		// [LAW:single-enforcer] NewBatch says what a batch is; records within
		// the frame's bound that it refuses for more than their absence are
		// lit2 broken.
		panic(fmt.Sprintf("store: the records of a frame within its bound are no batch: %v", err))
	}
	return wholeFrame{batch: batch, n: frameHeader + int(length)}
}
