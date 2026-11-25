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

// Scan is what a reader finds in a stream's bytes: whole frames; torn spans,
// bytes between whole frames that no command finished, skipped and counted,
// each with why the frame at its start was refused; End, the offset after
// the last whole frame; and Tail, the bytes after End, if any. The tail is
// never consumed and never truncated, since a frame cut short there and an
// append in flight look the same (the store's decision 4).
type Scan struct {
	Frames []Frame
	Torn   []Torn
	End    Offset
	Tail   ScanTail
}

// Until is the offset after the scan's last byte: the tail's end, or End
// when there is no tail.
func (s Scan) Until() Offset {
	switch t := s.Tail.(type) {
	case NoTail:
		return s.End
	case InFlight:
		return t.Span.To
	case TornTail:
		return t.Span.To
	}
	panic(fmt.Sprintf("store: %T is not a ScanTail", s.Tail))
}

// Torn is a torn span, and why the frame at its start was refused; magic
// later in the span is only bytes.
type Torn struct {
	Span
	Why Refusal
}

// ScanTail is the bytes after a scan's last whole frame: none; a frame cut
// short, which more bytes could finish, as an append in flight would; or
// bytes that no more bytes can make a frame, and why.
//
//sumtype:decl
type ScanTail interface{ tail() }

// NoTail is a scan whose bytes end where its last whole frame ends.
type NoTail struct{}

// InFlight is a tail that starts a frame its bytes cut short.
type InFlight struct{ Span Span }

// TornTail is a tail whose frame is refused whatever follows it, and why.
type TornTail struct {
	Span Span
	Why  Malformed
}

func (NoTail) tail()   {}
func (InFlight) tail() {}
func (TornTail) tail() {}

// Refusal is why the bytes at a place are not a whole frame: Truncated, a
// frame its bytes cut short, or Malformed, one no more bytes can make
// whole.
//
//sumtype:decl
type Refusal interface{ refusal() }

// Truncated is a frame whose header or body runs past the bytes there.
type Truncated struct{}

// Malformed is why bytes are a frame that no more bytes can make whole.
//
//sumtype:decl
type Malformed interface {
	Refusal
	malformed()
}

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

func (NoMagic) malformed()         {}
func (OverBound) malformed()       {}
func (CRCMismatch) malformed()     {}
func (BadRecordLength) malformed() {}
func (NoRecords) malformed()       {}

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
	s := Scan{Frames: []Frame{}, Torn: []Torn{}}
	pos, end := 0, 0
	var why Refusal // why the frame at end was refused, once one was
	for pos < len(b) {
		batch, n, refused := frameAt(b[pos:])
		if refused != nil {
			if pos == end {
				why = refused
			}
			next := bytes.Index(b[pos+1:], frameMagic[:])
			if next < 0 {
				break
			}
			pos += 1 + next
			continue
		}
		if pos > end {
			s.Torn = append(s.Torn, Torn{Span: Span{From: from + Offset(end), To: from + Offset(pos)}, Why: why})
		}
		s.Frames = append(s.Frames, Frame{At: from + Offset(pos), Batch: batch})
		pos += n
		end = pos
	}
	s.End = from + Offset(end)
	s.Tail = tailOf(Span{From: s.End, To: from + Offset(len(b))}, why)
	return s
}

// tailOf is the tail over span, its frame refused for why when it has
// bytes.
func tailOf(span Span, why Refusal) ScanTail {
	if span.Len() == 0 {
		return NoTail{}
	}
	if m, malformed := why.(Malformed); malformed {
		return TornTail{Span: span, Why: m}
	}
	return InFlight{Span: span}
}

// frameAt reads the whole frame b starts with, and its length, or why b
// starts with none: a frame its bytes cut short, no magic, a length over
// the bound, a CRC that disagrees, a body that is not records, or one that
// holds none.
func frameAt(b []byte) (Batch, int, Refusal) {
	if !bytes.HasPrefix(b, frameMagic[:min(len(b), len(frameMagic))]) {
		return Batch{}, 0, NoMagic{}
	}
	if len(b) < frameHeader {
		return Batch{}, 0, Truncated{}
	}
	length := binary.BigEndian.Uint32(b[4:])
	if length > maxBody {
		return Batch{}, 0, OverBound{Length: length}
	}
	if frameHeader+int(length) > len(b) {
		return Batch{}, 0, Truncated{}
	}
	body := b[frameHeader : frameHeader+int(length)]
	if frameCRC(b[4:8], body) != binary.BigEndian.Uint32(b[8:]) {
		return Batch{}, 0, CRCMismatch{}
	}
	var rs []Record
	for len(body) > 0 {
		if len(body) < recordHeader {
			return Batch{}, 0, BadRecordLength{}
		}
		n := binary.BigEndian.Uint32(body)
		if uint64(n) > uint64(len(body)-recordHeader) {
			return Batch{}, 0, BadRecordLength{}
		}
		rs = append(rs, Record(body[recordHeader:recordHeader+int(n)]))
		body = body[recordHeader+int(n):]
	}
	// the body is bounded by maxBody, so the one refusal NewBatch has left
	// here is a batch of no records
	batch, err := NewBatch(rs...)
	if err != nil {
		return Batch{}, 0, NoRecords{}
	}
	return batch, frameHeader + int(length), nil
}
