package store

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"path"
	"strconv"
	"strings"
)

// The store's layout under its root, <git-common-dir>/lit/store/ (D11):
// log/<checkout-id> per writing checkout and repo/ for lit's repository.
// The layout is the one builder of a Path.
const (
	logDir Path = "log"
	// repoDir is lit's repository's directory, a bare repository git keeps.
	repoDir = "repo"
)

// LogPath is the log of the checkout id names.
func LogPath(id CheckoutID) Path { return Path(path.Join(string(logDir), id.String())) }

// logOwner is the checkout whose log p is.
func logOwner(p Path) (CheckoutID, error) {
	return ParseCheckoutID(path.Base(string(p)))
}

// The log header: magic, format version, the LogID, CRC-32C over the three.
// Stream offsets count from after it.
const (
	headerSize = 4 + 4 + 16 + 4
	logVersion = 1
)

var logMagic = [4]byte{'l', 'i', 't', 'L'}

func logHeader(id LogID) []byte {
	h := make([]byte, 0, headerSize)
	h = append(h, logMagic[:]...)
	h = binary.BigEndian.AppendUint32(h, logVersion)
	h = append(h, id[:]...)
	return binary.BigEndian.AppendUint32(h, crc32.Checksum(h, castagnoli))
}

func parseLogHeader(h []byte) (LogID, error) {
	var id LogID
	switch {
	case len(h) < headerSize:
		return id, fmt.Errorf("the log header is %d bytes; it is %d", len(h), headerSize)
	case !bytes.Equal(h[:4], logMagic[:]):
		return id, errors.New("the log does not start with a log header")
	case binary.BigEndian.Uint32(h[4:]) != logVersion:
		return id, fmt.Errorf("the log is format %d; this lit2 reads %d", binary.BigEndian.Uint32(h[4:]), logVersion)
	case crc32.Checksum(h[:headerSize-4], castagnoli) != binary.BigEndian.Uint32(h[headerSize-4:]):
		return id, errors.New("the log header's CRC disagrees with it")
	}
	copy(id[:], h[8:24])
	return id, nil
}

// segmentName is a segment's tree entry: its start offset in 16 hex
// digits, so names sort in stream order.
func segmentName(at Offset) string { return fmt.Sprintf("%016x", uint64(at)) }

func parseSegmentName(s string) (Offset, error) {
	at, err := strconv.ParseUint(s, 16, 64)
	if err != nil || segmentName(Offset(at)) != s {
		return 0, fmt.Errorf("%q is not a segment's start offset", s)
	}
	return Offset(at), nil
}

// The commit message names the log, so a rewritten stream is a new LogID.
const messagePrefix = "lit-log v1 "

func commitMessage(id LogID) string { return messagePrefix + id.String() + "\n" }

func parseCommitMessage(m string) (LogID, error) {
	s, ok := strings.CutPrefix(m, messagePrefix)
	id, okID := token(strings.TrimSuffix(s, "\n"))
	if !ok || !okID || !strings.HasSuffix(s, "\n") {
		return LogID{}, fmt.Errorf("%q is not a lit-log commit message", m)
	}
	return id, nil
}
