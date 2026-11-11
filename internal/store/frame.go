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

// NewBatch is the batch of rs, refusing none and refusing more bytes than
// one frame carries.
func NewBatch(rs ...Record) (Batch, error) {
	if len(rs) == 0 {
		return Batch{}, errors.New("a batch holds at least one record")
	}
	b := Batch{first: rs[0], rest: rs[1:]}
	if n := b.bodySize(); n > maxBody {
		return Batch{}, fmt.Errorf("a batch of %d bytes is over the %d a frame carries", n, maxBody)
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
// bytes between whole frames that no command finished, skipped and counted;
// and End, the offset after the last whole frame. The bytes from End to
// Until are the incomplete tail: never consumed, never truncated, since a
// torn tail and an append in flight look the same (the store's decision 4).
type Scan struct {
	Frames []Frame
	Torn   []Span
	End    Offset
	Until  Offset
}

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
	s := Scan{Frames: []Frame{}, Torn: []Span{}}
	pos, end := 0, 0
	for pos < len(b) {
		batch, n, ok := frameAt(b[pos:])
		if !ok {
			next := bytes.Index(b[pos+1:], frameMagic[:])
			if next < 0 {
				break
			}
			pos += 1 + next
			continue
		}
		if pos > end {
			s.Torn = append(s.Torn, Span{From: from + Offset(end), To: from + Offset(pos)})
		}
		s.Frames = append(s.Frames, Frame{At: from + Offset(pos), Batch: batch})
		pos += n
		end = pos
	}
	s.End, s.Until = from+Offset(end), from+Offset(len(b))
	return s
}

// frameAt reads the whole frame b starts with, and its length, or reports
// that b starts with none: no magic, a length over the bound or past the
// bytes there, a CRC that disagrees, or a body that is not records.
func frameAt(b []byte) (Batch, int, bool) {
	if len(b) < frameHeader || !bytes.Equal(b[:4], frameMagic[:]) {
		return Batch{}, 0, false
	}
	length := binary.BigEndian.Uint32(b[4:])
	if length > maxBody || frameHeader+int(length) > len(b) {
		return Batch{}, 0, false
	}
	body := b[frameHeader : frameHeader+int(length)]
	if frameCRC(b[4:8], body) != binary.BigEndian.Uint32(b[8:]) {
		return Batch{}, 0, false
	}
	var rs []Record
	for len(body) > 0 {
		if len(body) < recordHeader {
			return Batch{}, 0, false
		}
		n := binary.BigEndian.Uint32(body)
		if uint64(n) > uint64(len(body)-recordHeader) {
			return Batch{}, 0, false
		}
		rs = append(rs, Record(body[recordHeader:recordHeader+int(n)]))
		body = body[recordHeader+int(n):]
	}
	batch, err := NewBatch(rs...)
	return batch, frameHeader + int(length), err == nil
}
