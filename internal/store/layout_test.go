package store

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"testing"
)

func TestALogHeaderReadsBackOrIsRefused(t *testing.T) {
	id := LogID{0xa, 0xb}
	h := logHeader(id)
	if got, err := parseLogHeader(h); err != nil || got != id {
		t.Fatalf("parseLogHeader(logHeader(%s)) = %s, %v; want it back", id, got, err)
	}
	// each case differs from the control in the one respect refused
	edit := func(f func(b []byte) []byte) []byte { return f(append([]byte{}, h...)) }
	for name, c := range map[string]struct {
		h    []byte
		want error
	}{
		"short":    {h[:headerSize-1], errShortLogHeader},
		"no magic": {edit(func(b []byte) []byte { b[0] ^= 1; return b }), errNotALog},
		"another format": {edit(func(b []byte) []byte {
			binary.BigEndian.PutUint32(b[4:], logVersion+1)
			binary.BigEndian.PutUint32(b[headerSize-4:], crc32.Checksum(b[:headerSize-4], castagnoli))
			return b
		}), errLogFormat},
		"a CRC that disagrees": {edit(func(b []byte) []byte { b[8] ^= 1; return b }), errLogHeaderCRC},
	} {
		if got, err := parseLogHeader(c.h); !errors.Is(err, c.want) {
			t.Errorf("%s: parseLogHeader = %s, %v; want %v", name, got, err, c.want)
		}
	}
}
