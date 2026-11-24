package store

import (
	"errors"
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
	from  git.Was
	log   LogID
	segs  []Segment
	prior Prior
}

// NewPublication is the publication of segs on ref for log, replacing the
// ref from what it was when its caller read it, refusing segments that do
// not tile and no segment at all, which would publish an empty tree. Its
// caller built every segment, so it read no prior publication.
func NewPublication(ref Ref, from git.Was, log LogID, segs []Segment) (Publication, error) {
	if len(segs) == 0 {
		return Publication{}, fmt.Errorf("a publication of %s %w", ref.Name(), errNoSegment)
	}
	spans := make([]Span, len(segs))
	for i, s := range segs {
		spans[i] = span(s)
	}
	if err := tile(spans); err != nil {
		return Publication{}, err
	}
	return Publication{ref: ref, from: from, log: log, segs: segs, prior: NoPrior{}}, nil
}

// The refusals of a publication's segments: none at all, one that does not
// start where the stream before it ends, which is a gap, an overlap or a
// late start, and one that holds no bytes.
var (
	errNoSegment        = errors.New("holds no segment")
	errMisplacedSegment = errors.New("not where the stream before it ends")
	errEmptySegment     = errors.New("holds no bytes")
)

// tile refuses spans that do not tile a stream from 0, each holding bytes.
// A publication sealed here and one read back from a commit are both
// checked by it alone `[LAW:single-enforcer]`.
func tile(spans []Span) error {
	var at Offset
	for i, sp := range spans {
		if sp.From != at {
			return fmt.Errorf("segment %d starts at %d, %w, at %d", i, sp.From, errMisplacedSegment, at)
		}
		if sp.To <= sp.From {
			return fmt.Errorf("segment %d at %d %w", i, sp.From, errEmptySegment)
		}
		at = sp.To
	}
	return nil
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

// Resealed is a ref whose publication could not be kept, holding none or
// of another log, so sealing started at 0: the log derives it again
// (the store's decision 11), and Why says why
// `[LAW:nothing-unseen]`.
type Resealed struct{ Why error }

func (KeptFrom) prior() {}
func (NoPrior) prior()  {}
func (Resealed) prior() {}

// SegmentBound is the most bytes a sealed segment holds, unless one frame
// alone is larger. It bounds what a publish re-sends; it is not a wire
// format (the store's decision 9). A lowered bound reseals, once, every
// segment from the first one longer than it; a raised bound keeps the
// smaller segments already published and cuts only new bytes by it.
type SegmentBound uint32

// DefaultSegmentBound is 1 MiB, the bound the composition root passes.
const DefaultSegmentBound SegmentBound = 1 << 20

// Tail is a log's bytes from where sealing resumes, which keepWithin says.
type Tail struct {
	From  Offset
	Bytes []byte
}

// Seal cuts a stream into segments: the prior publication's full segments
// as they are, then the tail's whole frames and torn spans, greedily, each
// segment taking units while it stays within bound, a unit over the bound
// alone. Cuts depend only on the log's bytes, so sealing from what
// keepWithin keeps of a prior publication equals sealing from scratch
// under a bound no higher than the prior one's, and the last segment is
// the open one. The tail's incomplete end is not sealed.
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

// keepWithin is what sealing under bound keeps of a prior publication's
// segments, in order: its full segments up to the first one longer than
// bound, and the offset sealing resumes from, that segment's start or the
// open segment's. Under the bound they were cut by, or a lower one, the
// kept segments are the ones sealing from scratch would cut.
func keepWithin(prior []Kept, bound SegmentBound) ([]Kept, Offset) {
	if len(prior) == 0 {
		return nil, 0
	}
	full := prior[:len(prior)-1]
	for i, k := range full {
		if k.Span.Len() > uint64(bound) {
			return full[:i], k.Span.From
		}
	}
	return full, prior[len(prior)-1].Span.From
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
