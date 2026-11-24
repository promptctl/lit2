package wire_test

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/panics"
)

// message is a decoder that knows tags 1 (a varint) and 2 (a list of
// strings), as an older binary's decoder would.
type message struct {
	n    uint64
	list []string
	rest wire.Unknown
}

func decode(b []byte) (message, error) {
	var m message
	r := wire.NewReader(b)
	for tag, ok := r.Next(); ok; tag, ok = r.Next() {
		switch tag {
		case 1:
			m.n = r.Uvarint()
		case 2:
			m.list = append(m.list, string(r.Bytes()))
		}
	}
	m.rest = r.Rest()
	return m, r.Err()
}

func (m message) encode() []byte {
	var w wire.Writer
	w.Uvarint(1, m.n)
	for _, s := range m.list {
		w.Bytes(2, []byte(s))
	}
	w.Keep(m.rest)
	return w.Encoded()
}

func TestAnOlderDecoderKeepsWhatANewerEncoderWrote(t *testing.T) {
	var newer wire.Writer
	newer.Uvarint(1, 300)
	newer.Bytes(2, []byte("b"))
	newer.Bytes(7, []byte("newer's text"))
	newer.Bytes(2, []byte("a"))
	newer.Uvarint(1<<29-1, 1<<63)
	newer.Bytes(2, []byte("c"))
	newer.Bytes(8, nil)

	m, err := decode(newer.Encoded())
	if err != nil || m.n != 300 || !slices.Equal(m.list, []string{"b", "a", "c"}) {
		t.Fatalf("the older decoder read %+v, %v; want 300 and the list in stored order", m, err)
	}
	m.list = append(m.list, "d")
	var want wire.Writer
	want.Uvarint(1, 300)
	for _, s := range []string{"b", "a", "c", "d"} {
		want.Bytes(2, []byte(s))
	}
	want.Bytes(7, []byte("newer's text"))
	want.Uvarint(1<<29-1, 1<<63)
	want.Bytes(8, nil)
	if got := m.encode(); !bytes.Equal(got, want.Encoded()) {
		t.Errorf("written back, the message is %x; want the known fields, then the unknown ones in stored order, %x", got, want.Encoded())
	}
}

func TestTheBytesAreProtobufs(t *testing.T) {
	var w wire.Writer
	w.Uvarint(1, 150)
	w.Bytes(2, []byte("testing"))
	w.Bytes(16, []byte{})
	// protobuf's encoding guide: 08 96 01 is field 1 = 150, 12 07 … is
	// field 2 = "testing", and field 16's key takes two bytes, 82 01
	want := append([]byte{0x08, 0x96, 0x01, 0x12, 0x07}, "testing"...)
	want = append(want, 0x82, 0x01, 0x00)
	if !bytes.Equal(w.Encoded(), want) {
		t.Errorf("the message is %x; want protobuf's %x", w.Encoded(), want)
	}
}

func TestTornOrForeignBytesStopTheReader(t *testing.T) {
	// the control: field 1 = 150, whole, reads
	if m, err := decode([]byte{0x08, 0x96, 0x01}); err != nil || m.n != 150 {
		t.Fatalf("a whole message decodes as %+v, %v; the control must read", m, err)
	}
	for name, c := range map[string]struct {
		b    []byte
		want error
	}{
		"a key cut short":           {[]byte{0x80}, wire.ErrKeyNotVarint},
		"a varint cut short":        {[]byte{0x08, 0x96}, wire.ErrTornVarint},
		"a length past the message": {[]byte{0x12, 0x07, 't'}, wire.ErrLengthPast},
		"field number 0":            {[]byte{0x00, 0x01}, wire.ErrFieldNumber},
		"an I64 field":              {[]byte{0x09, 1, 2, 3, 4, 5, 6, 7, 8}, wire.ErrWireType},
		"an I32 field":              {[]byte{0x0d, 1, 2, 3, 4}, wire.ErrWireType},
		"a group":                   {[]byte{0x0b, 0x0c}, wire.ErrWireType},
		"a tag past protobuf's":     {[]byte{0x80, 0x80, 0x80, 0x80, 0x10, 0x00}, wire.ErrFieldNumber},
	} {
		if m, err := decode(c.b); !errors.Is(err, c.want) {
			t.Errorf("%s decodes as %+v, %v; want the reader stopped with %v", name, m, err, c.want)
		}
	}
	// a field read as the wire type it is not
	var w wire.Writer
	w.Bytes(1, []byte("not a number"))
	if m, err := decode(w.Encoded()); !errors.Is(err, wire.ErrReadAsOther) {
		t.Errorf("a LEN read as a varint decodes as %+v, %v; want the reader stopped", m, err)
	}
}

func TestAReaderStopsAtItsFirstError(t *testing.T) {
	var w wire.Writer
	w.Bytes(1, []byte("x"))
	w.Uvarint(3, 9)
	r := wire.NewReader(w.Encoded())
	tag, ok := r.Next()
	if !ok || tag != 1 {
		t.Fatalf("the first field is %d, %v", tag, ok)
	}
	r.Uvarint()
	if tag, ok := r.Next(); ok || !errors.Is(r.Err(), wire.ErrReadAsOther) {
		t.Errorf("after an error the reader moved on to field %d, %v; want it stopped", tag, r.Err())
	}
}

func TestAFieldNumberOutsideProtobufsPanics(t *testing.T) {
	var control wire.Writer
	control.Uvarint(1<<29-1, 1) // protobuf's largest field number is the control that writes
	for _, tag := range []uint32{0, 1 << 29} {
		panics.With(t, wire.ErrFieldNumber, func() {
			var w wire.Writer
			w.Uvarint(tag, 1)
		})
	}
}
