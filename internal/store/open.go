package store

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"

	"github.com/promptctl/lit2/internal/git"
)

// Ports is everything the store touches: its files, lit's repository, and
// entropy for a new log's id. The disk and the twins are two sets of the
// same ports, and the Store over them is one type.
type Ports struct {
	Files   Files
	Objects git.Objects
	Refs    git.Refs
	Entropy io.Reader
}

// DiskPorts is the store of at's clone on disk, rooted at
// <git-common-dir>/lit/store/ (D11), lit's repository at repo/ beneath it.
// It reads nothing; every worktree of a clone builds the same ports.
func DiskPorts(at git.Checkout, entropy io.Reader) Ports {
	root := filepath.Join(at.SharedDir(), "store")
	repo := git.RepositoryAt(filepath.Join(root, repoDir))
	return Ports{Files: FilesAt(root), Objects: repo, Refs: repo, Entropy: entropy}
}

// Store is one clone's store as one checkout sees it: every writer's
// stream to read, and this checkout's own log to append to and publish. It
// takes no lock and reads no clock; every effect is a port's.
type Store struct {
	me    CheckoutID
	ports Ports
	bound SegmentBound
}

// Open is the store over p for the checkout me, sealing segments within
// bound. It performs no I/O.
func Open(me CheckoutID, p Ports, bound SegmentBound) *Store {
	return &Store{me: me, ports: p, bound: bound}
}

// Appended is what an append wrote: its records and bytes, the position of
// the log's end after it, and whether this append created the log.
type Appended struct {
	Records, Bytes int
	Until          Position
	Log            Made
}

// Append writes one command's batch to this checkout's log as one frame:
// the only local store write a command makes, since every other file is
// derived. The first append creates the log, so exactly one header
// survives two first appends racing.
func (s *Store) Append(b Batch) (Appended, error) {
	p := LogPath(s.me)
	log, opened, err := s.ownLog(p)
	if err != nil {
		return Appended{}, err
	}
	frame := Encode(b)
	end, err := s.ports.Files.Append(p, frame)
	if err != nil {
		return Appended{}, err
	}
	return Appended{Records: len(b.Records()), Bytes: len(frame), Until: Position{Log: log, At: Offset(end - headerSize)}, Log: opened}, nil
}

// ownLog is the id of the log at p, created with a new id when there is
// none. The log is read before it is appended to, so bytes are never
// added to a file that is not a log.
func (s *Store) ownLog(p Path) (LogID, Made, error) {
	id, err := s.logID(p)
	if !errors.As(err, new(NotFound)) {
		return id, Existing, err
	}
	var fresh LogID
	if _, err := io.ReadFull(s.ports.Entropy, fresh[:]); err != nil {
		return LogID{}, 0, fmt.Errorf("minting a log id: %w", err)
	}
	opened, err := s.ports.Files.CreateOnce(p, logHeader(fresh))
	if err != nil {
		return LogID{}, 0, err
	}
	id, err = s.logID(p)
	return id, opened, err
}

func (s *Store) logID(p Path) (LogID, error) {
	h, err := s.ports.Files.ReadAt(p, 0, headerSize)
	if err != nil {
		return LogID{}, err
	}
	id, err := parseLogHeader(h)
	if err != nil {
		return LogID{}, fmt.Errorf("%s: %w", p, err)
	}
	return id, nil
}

// Stream is one writer's or inbox's events as the fold reads them, up to
// its head. Where its bytes come from, a log file or a publication's
// segments, is unexported, so the fold cannot branch on it
// `[LAW:types-are-the-program]`.
type Stream struct {
	Ref   Ref
	Until Position
	src   source
}

//sumtype:decl
type source interface{ source() }

// logFile is a stream read from a log under this common dir.
type logFile struct{ path Path }

// published is a stream read from a publication's segments, in stream
// order; a segment ends where the next starts, the last at end.
type published struct {
	segs []placed
	end  Offset
}

type placed struct {
	start Offset
	blob  git.ObjectID
}

func (logFile) source()   {}
func (published) source() {}

// Unreadable is a ref or a log that is not a stream: a ref name ParseRef
// refuses, a ref naming objects that are missing or not lit's, a torn ref,
// or a log whose header does not read. Name is the ref name or the log's
// path.
type Unreadable struct {
	Name string
	Why  error
}

// Heads is every stream of the clone, sorted by ref name, and everything
// that should have been one and is not. RefsRead and LogsRead count what
// was listed, so a fresh store reads zero of each.
type Heads struct {
	Streams            []Stream
	Unreadable         []Unreadable
	RefsRead, LogsRead int
}

// Heads lists refs/lit/ and every log under this common dir. A writer with
// a log here is read from its log, of which its own publication is a
// prefix; every other ref is read from its commit's segments. Reading
// every local log, not only this checkout's, makes a sibling worktree's
// writes visible whether or not its sync has run
// `[LAW:no-ambient-temporal-coupling]`.
func (s *Store) Heads() (Heads, error) {
	refs, bad, err := s.ports.Refs.List(litRefs)
	if err != nil {
		return Heads{}, err
	}
	logs, err := s.ports.Files.List(logDir)
	if err != nil {
		return Heads{}, err
	}
	h := Heads{Streams: []Stream{}, Unreadable: []Unreadable{}, RefsRead: len(refs) + len(bad), LogsRead: len(logs)}
	unreadable := func(name string, why error) { h.Unreadable = append(h.Unreadable, Unreadable{Name: name, Why: why}) }
	local := map[Ref]bool{}
	for _, p := range logs {
		st, err := s.logStream(p)
		if err != nil {
			unreadable(string(p), err)
			continue
		}
		local[st.Ref] = true
		h.Streams = append(h.Streams, st)
	}
	for _, r := range refs {
		ref, err := ParseRef(r.Name)
		if err != nil {
			unreadable(string(r.Name), err)
			continue
		}
		if local[ref] {
			continue // read from its log, which the publication is a prefix of
		}
		pub, log, err := s.publication(r.Target)
		if err != nil {
			unreadable(string(r.Name), err)
			continue
		}
		h.Streams = append(h.Streams, Stream{Ref: ref, Until: Position{Log: log, At: pub.end}, src: pub})
	}
	for _, b := range bad {
		unreadable(string(b.Name), b.Why)
	}
	slices.SortFunc(h.Streams, func(a, b Stream) int { return cmp.Compare(a.Ref.Name(), b.Ref.Name()) })
	slices.SortFunc(h.Unreadable, func(a, b Unreadable) int { return cmp.Compare(a.Name, b.Name) })
	return h, nil
}

// logStream is the stream of the log at p, up to its size now.
func (s *Store) logStream(p Path) (Stream, error) {
	owner, err := logOwner(p)
	if err != nil {
		return Stream{}, err
	}
	log, err := s.logID(p)
	if err != nil {
		return Stream{}, err
	}
	size, err := s.ports.Files.Size(p)
	if err != nil {
		return Stream{}, err
	}
	return Stream{Ref: WriterRef{Checkout: owner}, Until: Position{Log: log, At: Offset(size - headerSize)}, src: logFile{path: p}}, nil
}

// publication reads the publication a commit holds: its log, from the
// message, and its segments, from the tree's names, each one's span
// checked against its blob's size: segments that do not tile the stream
// from 0 make no publication.
func (s *Store) publication(commit git.ObjectID) (published, LogID, error) {
	c, err := s.ports.Objects.Commit(commit)
	if err != nil {
		return published{}, LogID{}, err
	}
	log, err := parseCommitMessage(c.Message)
	if err != nil {
		return published{}, LogID{}, err
	}
	es, err := s.ports.Objects.Tree(c.Tree)
	if err != nil {
		return published{}, LogID{}, err
	}
	pub := published{segs: make([]placed, len(es))}
	for i, e := range es {
		at, err := parseSegmentName(e.Name)
		if err != nil {
			return published{}, LogID{}, fmt.Errorf("tree %s: %w", c.Tree, err)
		}
		if at != pub.end {
			return published{}, LogID{}, fmt.Errorf("tree %s: segment %s starts at %d, not at %d where the stream before it ends", c.Tree, e.Name, at, pub.end)
		}
		// the header alone, so reading heads inflates no segment
		size, err := s.ports.Objects.BlobSize(e.Blob)
		if err != nil {
			return published{}, LogID{}, fmt.Errorf("tree %s: segment %s: %w", c.Tree, e.Name, err)
		}
		if size == 0 {
			return published{}, LogID{}, fmt.Errorf("tree %s: segment %s holds no bytes", c.Tree, e.Name)
		}
		pub.segs[i] = placed{start: at, blob: e.Blob}
		pub.end = at + Offset(size)
	}
	return pub, log, nil
}

// Read reads a range of a stream, from From to the stream's head as Heads
// saw it, so what a fold consumes matches the heads it planned against.
func (s *Store) Read(r Range) (Scan, error) {
	if r.From > r.Stream.Until.At {
		return Scan{}, fmt.Errorf("%s: reading from %d, past its head at %d", r.Stream.Ref.Name(), r.From, r.Stream.Until.At)
	}
	b, err := s.bytes(r.Stream.src, Span{From: r.From, To: r.Stream.Until.At})
	if err != nil {
		return Scan{}, fmt.Errorf("%s: %w", r.Stream.Ref.Name(), err)
	}
	return ScanFrames(r.From, b), nil
}

func (s *Store) bytes(src source, want Span) ([]byte, error) {
	switch src := src.(type) {
	case logFile:
		return s.ports.Files.ReadAt(src.path, headerSize+uint64(want.From), want.Len())
	case published:
		b := make([]byte, 0, want.Len())
		for i, seg := range src.segs {
			end := src.end
			if i+1 < len(src.segs) {
				end = src.segs[i+1].start
			}
			if end <= want.From || seg.start >= want.To {
				continue // outside the range read
			}
			blob, err := s.ports.Objects.Blob(seg.blob)
			if err != nil {
				return nil, err
			}
			if Offset(len(blob)) != end-seg.start {
				return nil, fmt.Errorf("segment at %d is %d bytes; the next starts %d bytes after it", seg.start, len(blob), end-seg.start)
			}
			b = append(b, blob[max(want.From, seg.start)-seg.start:min(want.To, end)-seg.start]...)
		}
		return b, nil
	}
	panic(fmt.Sprintf("store: %T is not a source", src))
}

// Snapshot reads the cache snapshot. A file replaced while it is read does
// not read whole, which is a torn snapshot and one refold.
func (s *Store) Snapshot() (SnapshotFile, error) {
	size, err := s.ports.Files.Size(SnapshotPath)
	if errors.As(err, new(NotFound)) {
		return NoSnapshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	b, err := s.ports.Files.ReadAt(SnapshotPath, 0, size)
	if err != nil {
		return nil, err
	}
	return decodeSnapshot(b), nil
}

// Swapped is what a swap wrote.
type Swapped struct{ Bytes int }

// Swap replaces the snapshot whole by atomic rename; no lock, since a
// reader of a snapshot mid-replace sees one whole file or a torn one.
func (s *Store) Swap(tag FoldTag, upto []Mark, payload []byte) (Swapped, error) {
	b := encodeSnapshot(tag, upto, payload)
	if err := s.ports.Files.Replace(SnapshotPath, b); err != nil {
		return Swapped{}, err
	}
	return Swapped{Bytes: len(b)}, nil
}

// Sealing is what sealing a checkout's own log made: a publication, or
// nothing to publish.
//
//sumtype:decl
type Sealing interface{ sealing() }

// Publishing is what an own publish did: published, or found nothing to
// publish.
//
//sumtype:decl
type Publishing interface{ publishing() }

// NothingToPublish is a log that holds no whole frame yet. Its publish
// would be an empty tree, so it makes no tree, no commit and no ref, and
// an idle publish writes nothing from the first.
type NothingToPublish struct {
	Ref Ref
	Log LogID
}

func (Publication) sealing()         {}
func (NothingToPublish) sealing()    {}
func (Published) publishing()        {}
func (NothingToPublish) publishing() {}

// SealOwn is this checkout's log sealed into a publication: what
// keepWithin keeps of the prior publication, the rest cut from where it
// resumes. With no log there is nothing to seal, which is NotFound; a log
// shorter than its own publication is ShorterThanPublished.
func (s *Store) SealOwn() (Sealing, error) {
	ref := WriterRef{Checkout: s.me}
	st, err := s.logStream(LogPath(s.me))
	if err != nil {
		return nil, err
	}
	kept, from, prior, err := s.prior(ref, st.Until)
	if err != nil {
		return nil, err
	}
	tail, err := s.ports.Files.ReadAt(LogPath(s.me), headerSize+uint64(from), uint64(st.Until.At-from))
	if err != nil {
		return nil, err
	}
	segs := Seal(kept, Tail{From: from, Bytes: tail}, s.bound)
	if len(segs) == 0 {
		return NothingToPublish{Ref: ref, Log: st.Until.Log}, nil
	}
	p, err := NewPublication(ref, st.Until.Log, segs)
	if err != nil {
		return nil, err
	}
	p.prior = prior
	return p, nil
}

// ShorterThanPublished is a log that ends before its own publication of
// it does. A log only grows, so bytes the ref holds are gone from it, and
// sealing it would lose them.
type ShorterThanPublished struct {
	Ref            Ref
	Log, Published Offset
}

func (e ShorterThanPublished) Error() string {
	return fmt.Sprintf("the log of %s ends at %d, before its publication's end at %d", e.Ref.Name(), e.Log, e.Published)
}

// prior is what keepWithin keeps of ref's publication of the log now at
// until, where sealing resumes, and what was kept. A ref whose
// publication cannot be kept is resealed from 0, and the publish that
// follows replaces it (the store's decision 11).
func (s *Store) prior(ref Ref, until Position) ([]Kept, Offset, Prior, error) {
	h, err := s.head(ref)
	if err != nil {
		return nil, 0, nil, err
	}
	switch h := h.(type) {
	case unnamed:
		return nil, 0, NoPrior{}, nil
	case tornRef:
		return nil, 0, Resealed{Why: h.why}, nil
	case named:
		pub, log, err := s.publication(h.commit)
		switch {
		case err != nil:
			return nil, 0, Resealed{Why: fmt.Errorf("its publication %s does not read: %w", h.commit, err)}, nil
		case log != until.Log:
			return nil, 0, Resealed{Why: fmt.Errorf("its publication %s is of log %s, not %s", h.commit, log, until.Log)}, nil
		case pub.end > until.At:
			return nil, 0, nil, ShorterThanPublished{Ref: ref, Log: until.At, Published: pub.end}
		}
		segs := make([]Kept, len(pub.segs))
		for i, seg := range pub.segs {
			to := pub.end
			if i+1 < len(pub.segs) {
				to = pub.segs[i+1].start
			}
			segs[i] = Kept{Span: Span{From: seg.start, To: to}, Blob: seg.blob}
		}
		kept, from := keepWithin(segs, s.bound)
		return kept, from, KeptFrom{Commit: h.commit}, nil
	}
	panic(fmt.Sprintf("store: %T is not a refHead", h))
}

// refHead is what a ref holds: a commit, nothing, or a file that holds no
// object id.
//
//sumtype:decl
type refHead interface{ refHead() }

type (
	named   struct{ commit git.ObjectID }
	unnamed struct{}
	tornRef struct{ why error }
)

func (named) refHead()   {}
func (unnamed) refHead() {}
func (tornRef) refHead() {}

func (s *Store) head(ref Ref) (refHead, error) {
	heads, bad, err := s.ports.Refs.List(ref.Name())
	if err != nil {
		return nil, err
	}
	for _, h := range heads {
		if h.Name == ref.Name() {
			return named{commit: h.Target}, nil
		}
	}
	for _, b := range bad {
		if b.Name == ref.Name() {
			return tornRef{why: b.Why}, nil
		}
	}
	return unnamed{}, nil
}

// RefMove is what a publish did to its ref.
//
//sumtype:decl
type RefMove interface{ move() }

// RefUnchanged is a ref that already named the commit: nothing to send.
type RefUnchanged struct{}

// RefCreated is a ref that named no commit before: absent, or torn.
type RefCreated struct{}

// RefReplaced is a ref replaced from Previous, never advanced: the commit
// has no parent, since the events are the history.
type RefReplaced struct{ Previous git.ObjectID }

func (RefUnchanged) move() {}
func (RefCreated) move()   {}
func (RefReplaced) move()  {}

// Published is what a publish did: the commit its ref now names, the
// ref's move, the objects it added and their blob bytes, how many
// segments it kept and sealed, and what it kept of the publication before
// it. RefUnchanged with no new objects is a publish with nothing to send.
type Published struct {
	Commit       git.ObjectID
	Move         RefMove
	New          []git.ObjectID
	NewBytes     uint64
	Kept, Sealed int
	Prior        Prior
}

// Publish makes p the head of its ref as one parentless commit: its fresh
// segments stored as blobs, one tree of every segment named by its start
// offset, one commit whose message names the log, and the ref replaced
// unless it names that commit already. The commit is a pure function of
// the tree, so racing publishes of one log converge on one id.
func (s *Store) Publish(p Publication) (Published, error) {
	out := Published{New: []git.ObjectID{}, Prior: p.prior}
	entries := make([]git.Entry, 0, len(p.segs))
	for _, seg := range p.segs {
		blob, err := s.store(seg, &out)
		if err != nil {
			return Published{}, err
		}
		entries = append(entries, git.Entry{Name: segmentName(span(seg).From), Blob: blob})
	}
	tree, stored, err := s.ports.Objects.PutTree(entries)
	if err != nil {
		return Published{}, err
	}
	out.added(tree, stored, 0)
	commit, stored, err := s.ports.Objects.PutCommit(git.Commit{Tree: tree, Message: commitMessage(p.log)})
	if err != nil {
		return Published{}, err
	}
	out.added(commit, stored, 0)
	h, err := s.head(p.ref)
	if err != nil {
		return Published{}, err
	}
	out.Commit, out.Move = commit, moved(h, commit)
	switch out.Move.(type) {
	case RefUnchanged:
	case RefCreated, RefReplaced:
		if err := s.ports.Refs.Replace(p.ref.Name(), commit); err != nil {
			return Published{}, err
		}
	}
	return out, nil
}

// moved is what pointing a ref holding h at to does to it.
func moved(h refHead, to git.ObjectID) RefMove {
	switch h := h.(type) {
	case named:
		if h.commit == to {
			return RefUnchanged{}
		}
		return RefReplaced{Previous: h.commit}
	case unnamed, tornRef:
		return RefCreated{}
	}
	panic(fmt.Sprintf("store: %T is not a refHead", h))
}

// store is a segment's blob: a kept one's id, or a fresh one's bytes put.
func (s *Store) store(seg Segment, out *Published) (git.ObjectID, error) {
	switch seg := seg.(type) {
	case Kept:
		out.Kept++
		return seg.Blob, nil
	case Fresh:
		out.Sealed++
		id, stored, err := s.ports.Objects.PutBlob(seg.Bytes)
		if err != nil {
			return git.ObjectID{}, err
		}
		out.added(id, stored, len(seg.Bytes))
		return id, nil
	}
	panic(fmt.Sprintf("store: %T is not a Segment", seg))
}

func (out *Published) added(id git.ObjectID, stored git.Stored, bytes int) {
	switch stored {
	case git.New:
		out.New = append(out.New, id)
		out.NewBytes += uint64(bytes)
	case git.Existing:
	}
}
