package store

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"

	"github.com/promptctl/lit2/internal/git"
)

// FoldTag is the fold's version, opaque here and compared byte-equal: a
// snapshot folded by another version is not resumed.
type FoldTag []byte

// Mark is how far a snapshot consumed one stream.
type Mark struct {
	Ref  Ref
	Upto Position
}

// SnapshotFile is the cache snapshot as read: none, torn, or saved. It is a
// disposable cache (INV:disposable-cache): every answer but Saved is a
// refold, never an error.
//
//sumtype:decl
type SnapshotFile interface{ file() }

// NoSnapshot is no snapshot file.
type NoSnapshot struct{}

// TornSnapshot is a snapshot file that does not read whole.
type TornSnapshot struct{ Why error }

// Saved is a snapshot: the fold version that wrote it, how far it consumed
// each stream, and the fold's payload.
type Saved struct {
	Tag     FoldTag
	Marks   []Mark
	Payload []byte
}

func (NoSnapshot) file()   {}
func (TornSnapshot) file() {}
func (Saved) file()        {}

// Base is where a fold starts: from genesis, for a reason, or from a saved
// snapshot's payload.
//
//sumtype:decl
type Base interface{ base() }

// Genesis is a fold from no state, every stream from 0.
type Genesis struct{ Why Miss }

// FromSaved is a fold resumed from a snapshot's payload.
type FromSaved struct{ Payload []byte }

func (Genesis) base()   {}
func (FromSaved) base() {}

// Miss is why a snapshot was not resumed.
//
//sumtype:decl
type Miss interface{ miss() }

// Absent is no snapshot file.
type Absent struct{}

// Torn is a snapshot file that did not read whole.
type Torn struct{ Why error }

// TagChanged is a snapshot folded by another fold version.
type TagChanged struct{}

// Rewritten is a stream the snapshot consumed whose log is not the one it
// read, or is shorter than it read: a rewrite replaced it.
type Rewritten struct{ Ref Ref }

// Withdrawn is a stream the snapshot consumed that is no longer there.
type Withdrawn struct{ Ref Ref }

func (Absent) miss()     {}
func (Torn) miss()       {}
func (TagChanged) miss() {}
func (Rewritten) miss()  {}
func (Withdrawn) miss()  {}

// Range is a stream's bytes from From to its head.
type Range struct {
	Stream Stream
	From   Offset
}

// Resume is where the fold starts and what it reads: Delta holds one Range
// per stream with bytes past what the base consumed, so a snapshot that
// matches every head is resumed with an empty Delta. Held is the snapshot
// file's marks that still hold, on every base: each names a stream present
// under the mark's log, at or past the mark, so the bytes below it are the
// ones the snapshot read. The store is the one judge of that
// `[LAW:single-enforcer]`; a fold trusts no other mark.
type Resume struct {
	Base  Base
	Held  []Mark
	Delta []Range
}

// Plan says where the fold resumes, given its version, the current heads
// and the saved snapshot. It is pure: the cache decision reads no file
// `[LAW:effects-at-boundaries]`.
func Plan(tag FoldTag, now Heads, file SnapshotFile) Resume {
	base, held := resumable(tag, now, file)
	from := map[Ref]Offset{}
	switch base.(type) {
	case FromSaved:
		for _, m := range held {
			from[m.Ref] = m.Upto.At
		}
	case Genesis:
	}
	delta := []Range{}
	for _, s := range now.Streams {
		if at := from[s.Ref]; at < s.Until.At {
			delta = append(delta, Range{Stream: s, From: at})
		}
	}
	return Resume{Base: base, Held: held, Delta: delta}
}

// resumable is the base a fold starts from and the marks of file that
// hold, every mark judged: a snapshot is resumed when its tag is the fold's
// and every mark holds, and a genesis says the first reason it is not. A
// base that is a snapshot reads each stream from its mark; a genesis reads
// every stream from 0.
func resumable(tag FoldTag, now Heads, file SnapshotFile) (Base, []Mark) {
	switch f := file.(type) {
	case NoSnapshot:
		return Genesis{Why: Absent{}}, []Mark{}
	case TornSnapshot:
		return Genesis{Why: Torn(f)}, []Mark{}
	case Saved:
		heads := map[Ref]Position{}
		for _, s := range now.Streams {
			heads[s.Ref] = s.Until
		}
		held := []Mark{}
		var miss Miss
		for _, m := range f.Marks {
			head, ok := heads[m.Ref]
			switch {
			case !ok:
				miss = firstMiss(miss, Withdrawn{Ref: m.Ref})
			case head.Log != m.Upto.Log || head.At < m.Upto.At:
				miss = firstMiss(miss, Rewritten{Ref: m.Ref})
			default:
				held = append(held, m)
			}
		}
		switch {
		case !bytes.Equal(f.Tag, tag):
			return Genesis{Why: TagChanged{}}, held
		case miss != nil:
			return Genesis{Why: miss}, held
		}
		return FromSaved{Payload: f.Payload}, held
	}
	panic(fmt.Sprintf("store: %T is not a SnapshotFile", file))
}

// firstMiss is the first of the misses: why, unless it is none yet.
func firstMiss(why, next Miss) Miss {
	if why != nil {
		return why
	}
	return next
}

// The snapshot file: magic, format version, the fold tag, the marks, the
// payload, then CRC-32C over everything before it.
var snapshotMagic = [4]byte{'l', 'i', 't', 'S'}

const snapshotVersion = 1

func encodeSnapshot(tag FoldTag, marks []Mark, payload []byte) []byte {
	b := append([]byte{}, snapshotMagic[:]...)
	b = binary.BigEndian.AppendUint32(b, snapshotVersion)
	b = binary.BigEndian.AppendUint32(b, uint32(len(tag)))
	b = append(b, tag...)
	b = binary.BigEndian.AppendUint32(b, uint32(len(marks)))
	for _, m := range marks {
		name := m.Ref.Name()
		b = binary.BigEndian.AppendUint16(b, uint16(len(name)))
		b = append(b, name...)
		b = append(b, m.Upto.Log[:]...)
		b = binary.BigEndian.AppendUint64(b, uint64(m.Upto.At))
	}
	b = binary.BigEndian.AppendUint64(b, uint64(len(payload)))
	b = append(b, payload...)
	return binary.BigEndian.AppendUint32(b, crc32.Checksum(b, castagnoli))
}

// decodeSnapshot reads a snapshot file: Saved when it reads whole, Torn
// with the reason when it does not.
func decodeSnapshot(b []byte) SnapshotFile {
	saved, err := parseSnapshot(b)
	if err != nil {
		return TornSnapshot{Why: err}
	}
	return saved
}

func parseSnapshot(b []byte) (Saved, error) {
	if len(b) < 4 || crc32.Checksum(b[:len(b)-4], castagnoli) != binary.BigEndian.Uint32(b[len(b)-4:]) {
		return Saved{}, errors.New("the snapshot's CRC disagrees with it")
	}
	c := cursor{b: b[:len(b)-4]}
	if !bytes.Equal(c.take(4), snapshotMagic[:]) || c.u32() != snapshotVersion {
		return Saved{}, errors.New("the snapshot is not a snapshot this lit2 reads")
	}
	s := Saved{Tag: FoldTag(c.take(int(c.u32()))), Marks: []Mark{}}
	seen := map[Ref]bool{}
	for n := c.u32(); n > 0 && c.err == nil; n-- {
		name := git.RefName(c.take(int(c.u16())))
		var log LogID
		copy(log[:], c.take(len(log)))
		at := Offset(c.u64())
		if c.err != nil {
			break
		}
		ref, err := ParseRef(name)
		if err != nil {
			return Saved{}, err
		}
		if seen[ref] {
			return Saved{}, fmt.Errorf("the snapshot marks %s twice", name)
		}
		seen[ref] = true
		s.Marks = append(s.Marks, Mark{Ref: ref, Upto: Position{Log: log, At: at}})
	}
	s.Payload = c.take(int(c.u64()))
	if c.err == nil && len(c.b) != 0 {
		return Saved{}, fmt.Errorf("the snapshot holds %d bytes past its payload", len(c.b))
	}
	return s, c.err
}

// cursor reads a snapshot's fields in order; the first read past its end
// sets err, and every read after it yields nothing.
type cursor struct {
	b   []byte
	err error
}

func (c *cursor) take(n int) []byte {
	if c.err != nil {
		return nil
	}
	if n < 0 || n > len(c.b) {
		c.err = errors.New("the snapshot ends inside a field")
		return nil
	}
	v := c.b[:n:n]
	c.b = c.b[n:]
	return v
}

func (c *cursor) u16() uint16 { return uint16(c.uint(2)) }
func (c *cursor) u32() uint32 { return uint32(c.uint(4)) }
func (c *cursor) u64() uint64 { return c.uint(8) }

func (c *cursor) uint(n int) uint64 {
	v := uint64(0)
	for _, x := range c.take(n) {
		v = v<<8 | uint64(x)
	}
	return v
}
