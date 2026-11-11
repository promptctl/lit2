// Package wire reads and writes protobuf-wire fields, keeping every field a
// reader does not know.
//
// It is the protobuf wire format's subset of two wire types, VARINT and LEN
// (length-delimited), over encoding/binary, so `protoc --decode_raw` reads
// any record lit2 writes. A repeated tag is the next element of a list, in
// stored order, as protobuf's own rule has it. A field a reader does not
// read is kept, in stored order, and a writer puts it back after the known
// fields, so a binary that decodes, changes and encodes a message keeps
// every field a newer binary wrote (INV:preserve-unknown).
package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
)

// Type is a field's wire type, the low three bits of its key.
type Type uint8

// The two wire types of the subset; protobuf's I64, I32 and groups are not
// in it.
const (
	Varint Type = 0
	Len    Type = 2
)

// field is one stored field: its tag, its wire type and its value's bytes,
// the encoded varint for Varint and the payload for Len.
type field struct {
	tag uint32
	typ Type
	raw []byte
}

// Unknown is the fields a reader did not read, in stored order.
type Unknown struct{ fields []field }

// maxTag is protobuf's largest field number.
const maxTag = 1<<29 - 1

// Writer encodes one message, field by field.
type Writer struct{ b []byte }

func (w *Writer) key(tag uint32, t Type) {
	if tag == 0 || tag > maxTag {
		panic(fmt.Sprintf("wire: %d is not a field number", tag))
	}
	w.b = binary.AppendUvarint(w.b, uint64(tag)<<3|uint64(t))
}

// Uvarint writes v as a VARINT field.
func (w *Writer) Uvarint(tag uint32, v uint64) {
	w.key(tag, Varint)
	w.b = binary.AppendUvarint(w.b, v)
}

// Bytes writes b as a LEN field: a string, bytes or a nested message.
func (w *Writer) Bytes(tag uint32, b []byte) {
	w.key(tag, Len)
	w.b = binary.AppendUvarint(w.b, uint64(len(b)))
	w.b = append(w.b, b...)
}

// Keep writes u's fields as they were stored, in stored order; an encoder
// calls it after its known fields.
func (w *Writer) Keep(u Unknown) {
	for _, f := range u.fields {
		w.key(f.tag, f.typ)
		if f.typ == Len {
			w.b = binary.AppendUvarint(w.b, uint64(len(f.raw)))
		}
		w.b = append(w.b, f.raw...)
	}
}

// Encoded is the message written so far.
func (w *Writer) Encoded() []byte { return slices.Clone(w.b) }

// Reader decodes one message, field by field. Its first error stops it:
// Next answers false from then on and Err says what it was, so a decoder
// reads its fields and checks Err once, after the loop.
type Reader struct {
	b       []byte
	at      int
	current field
	read    bool // the current field's value was read
	started bool
	rest    []field
	err     error
}

// NewReader reads the message b.
func NewReader(b []byte) *Reader { return &Reader{b: b} }

// Next moves to the next field and answers its tag, or false at the end of
// the message or at the first error. The field before it, unless its value
// was read, joins Rest.
func (r *Reader) Next() (tag uint32, ok bool) {
	if r.started && !r.read {
		r.rest = append(r.rest, r.current)
	}
	r.started, r.read = true, true // nothing is current until a field is read whole
	if r.err != nil || r.at == len(r.b) {
		return 0, false
	}
	k, n := binary.Uvarint(r.b[r.at:])
	if n <= 0 {
		return 0, r.fail(errors.New("a field key is not a varint"))
	}
	r.at += n
	num, t := k>>3, Type(k&7)
	if num == 0 || num > maxTag {
		return 0, r.fail(fmt.Errorf("%d is not a field number", num))
	}
	var raw []byte
	switch t {
	case Varint:
		_, n := binary.Uvarint(r.b[r.at:])
		if n <= 0 {
			return 0, r.fail(fmt.Errorf("field %d is not a varint", num))
		}
		raw = r.b[r.at : r.at+n]
		r.at += n
	case Len:
		size, n := binary.Uvarint(r.b[r.at:])
		if n <= 0 || size > uint64(len(r.b)-r.at-n) {
			return 0, r.fail(fmt.Errorf("field %d's length runs past the message", num))
		}
		r.at += n
		raw = r.b[r.at : r.at+int(size)]
		r.at += int(size)
	default:
		return 0, r.fail(fmt.Errorf("field %d has wire type %d, which is not VARINT or LEN", num, t))
	}
	r.current, r.read = field{tag: uint32(num), typ: t, raw: raw}, false
	return uint32(num), true
}

func (r *Reader) fail(err error) bool {
	if r.err == nil {
		r.err = err
	}
	return false
}

// Uvarint is the current field's value as a VARINT; a field of another
// wire type is the reader's error.
func (r *Reader) Uvarint() uint64 {
	if r.current.typ != Varint {
		r.fail(fmt.Errorf("field %d is not a varint", r.current.tag))
		return 0
	}
	r.read = true
	v, _ := binary.Uvarint(r.current.raw)
	return v
}

// Bytes is the current field's value as a LEN; a field of another wire
// type is the reader's error. The bytes alias the message.
func (r *Reader) Bytes() []byte {
	if r.current.typ != Len {
		r.fail(fmt.Errorf("field %d is not length-delimited", r.current.tag))
		return nil
	}
	r.read = true
	return r.current.raw
}

// Err is the error that stopped the reader, nil when it reached the end.
func (r *Reader) Err() error { return r.err }

// Rest is every field the decoder did not read, in stored order. It is
// whole once Next has answered false.
func (r *Reader) Rest() Unknown { return Unknown{fields: slices.Clone(r.rest)} }
