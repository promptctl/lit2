package wire_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/promptctl/lit2/internal/events/wire"
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
	for name, b := range map[string][]byte{
		"a key cut short":           {0x80},
		"a varint cut short":        {0x08, 0x96},
		"a length past the message": {0x12, 0x07, 't'},
		"field number 0":            {0x00, 0x01},
		"an I64 field":              {0x09, 1, 2, 3, 4, 5, 6, 7, 8},
		"an I32 field":              {0x0d, 1, 2, 3, 4},
		"a group":                   {0x0b, 0x0c},
		"a tag past protobuf's":     {0x80, 0x80, 0x80, 0x80, 0x10, 0x00},
	} {
		if m, err := decode(b); err == nil {
			t.Errorf("%s decodes as %+v; want the reader stopped", name, m)
		}
	}
	// a field read as the wire type it is not
	var w wire.Writer
	w.Bytes(1, []byte("not a number"))
	if m, err := decode(w.Encoded()); err == nil {
		t.Errorf("a LEN read as a varint decodes as %+v; want the reader stopped", m)
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
	if tag, ok := r.Next(); ok || r.Err() == nil {
		t.Errorf("after an error the reader moved on to field %d; want it stopped", tag)
	}
}

func TestAFieldNumberOutsideProtobufsPanics(t *testing.T) {
	for _, tag := range []uint32{0, 1 << 29} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("writing field %d did not panic", tag)
				}
			}()
			var w wire.Writer
			w.Uvarint(tag, 1)
		}()
	}
}
