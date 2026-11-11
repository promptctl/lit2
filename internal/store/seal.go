package store

import (
	"fmt"

	"github.com/promptctl/lit2/internal/git"
)

// Segment is one frame-aligned byte range of a writer's stream in a
// publication: one already in the object database, or new bytes to store.
// "Reuse this blob" and "store these bytes" are two cases, not one struct
// of maybes `[LAW:types-are-the-program]`.
//
//sumtype:decl
type Segment interface{ seg() }

// Kept is a segment already in the object database, unchanged.
type Kept struct {
	Span Span
	Blob git.ObjectID
}

// Fresh is a segment's bytes, which the publish stores.
type Fresh struct {
	Start Offset
	Bytes []byte
}

func (Kept) seg()  {}
func (Fresh) seg() {}

func span(s Segment) Span {
	switch s := s.(type) {
	case Kept:
		return s.Span
	case Fresh:
		return Span{From: s.Start, To: s.Start + Offset(len(s.Bytes))}
	}
	panic(fmt.Sprintf("store: %T is not a Segment", s))
}

// Publication is a stream as one parentless commit will hold it: its ref,
// its log and its segments, which tile the stream from 0 with no gap and
// no overlap. A writer's publication, an inbox's and a rewrite are all one
// Publication with different values `[LAW:one-type-per-behavior]`.
type Publication struct {
	ref   Ref
	log   LogID
	segs  []Segment
	prior Prior
}

// NewPublication is the publication of segs on ref for log, refusing
// segments that leave a gap, overlap, or hold no bytes. Its caller built
// every segment, so it read no prior publication.
func NewPublication(ref Ref, log LogID, segs []Segment) (Publication, error) {
	var at Offset
	for i, s := range segs {
		sp := span(s)
		if sp.From != at {
			return Publication{}, fmt.Errorf("segment %d starts at %d, not at %d where the stream before it ends", i, sp.From, at)
		}
		if sp.To <= sp.From {
			return Publication{}, fmt.Errorf("segment %d at %d holds no bytes", i, sp.From)
		}
		at = sp.To
	}
	return Publication{ref: ref, log: log, segs: segs, prior: NoPrior{}}, nil
}

// Prior is what a publication kept of the one its ref held before it.
//
//sumtype:decl
type Prior interface{ prior() }

// KeptFrom is a prior publication of the same log, whose full segments
// were kept.
type KeptFrom struct{ Commit git.ObjectID }

// NoPrior is no prior publication read: the ref named none, or the caller
// built every segment.
type NoPrior struct{}

// Resealed is a ref whose publication could not be kept, torn, unreadable
// or of another log, so sealing started at 0: the log derives it again
// (the store's decision 11), and Why says why
// `[LAW:nothing-unseen]`.
type Resealed struct{ Why error }

func (KeptFrom) prior() {}
func (NoPrior) prior()  {}
func (Resealed) prior() {}

// SegmentBound is the most bytes a sealed segment holds, unless one frame
// alone is larger. It bounds what a publish re-sends; it is not a wire
// format, so a changed bound re-seals once (the store's decision 9).
type SegmentBound uint32

// DefaultSegmentBound is 1 MiB, the bound the composition root passes.
const DefaultSegmentBound SegmentBound = 1 << 20

// Tail is a log's bytes from the start of its prior publication's open
// segment, the last one, which sealing cuts again.
type Tail struct {
	From  Offset
	Bytes []byte
}

// Seal cuts a stream into segments: the prior publication's full segments
// as they are, then the tail's whole frames and torn spans, greedily, each
// segment taking units while it stays within bound, a unit over the bound
// alone. Cuts depend only on the log's bytes, so sealing from a prior
// publication equals sealing from scratch, and the last segment is the
// open one. The tail's incomplete end is not sealed.
func Seal(prior []Kept, tail Tail, bound SegmentBound) []Segment {
	segs := make([]Segment, 0, len(prior)+1)
	for _, k := range prior {
		segs = append(segs, k)
	}
	scan := ScanFrames(tail.From, tail.Bytes)
	fresh := func(sp Span) Fresh {
		return Fresh{Start: sp.From, Bytes: tail.Bytes[sp.From-tail.From : sp.To-tail.From]}
	}
	cut := tail.From
	for _, u := range units(scan) {
		if u.From > cut && u.To-cut > Offset(bound) {
			segs = append(segs, fresh(Span{From: cut, To: u.From}))
			cut = u.From
		}
	}
	if scan.End > cut {
		segs = append(segs, fresh(Span{From: cut, To: scan.End}))
	}
	return segs
}

// units is the scan's whole frames and torn spans in stream order, which
// tile its bytes up to End.
func units(s Scan) []Span {
	us := make([]Span, 0, len(s.Frames)+len(s.Torn))
	torn := s.Torn
	for _, f := range s.Frames {
		if len(torn) > 0 && torn[0].To == f.At {
			us = append(us, torn[0])
			torn = torn[1:]
		}
		us = append(us, Span{From: f.At, To: f.At + Offset(frameHeader+f.Batch.bodySize())})
	}
	return us
}
