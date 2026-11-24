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
// <git-common-dir>/lit/store/ (D11), lit's repository at repo/ beneath it,
// kept by g. It reads nothing; every worktree of a clone builds the same
// ports.
func DiskPorts(g *git.Git, at git.Checkout, entropy io.Reader) Ports {
	root := filepath.Join(at.SharedDir(), "store")
	repo := g.RepositoryAt(filepath.Join(root, repoDir))
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
// order, each with the span publication read it to hold, which tile the
// stream to end.
type published struct {
	segs []Kept
	end  Offset
}

func (logFile) source()   {}
func (published) source() {}

// Unreadable is a ref or a log that is not a stream: a ref name ParseRef
// refuses, a ref naming objects that are missing or not lit's, or a log whose header does not read. Name is the ref name or the log's
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
	refs, err := s.ports.Refs.List(litRefs)
	if err != nil {
		return Heads{}, err
	}
	logs, err := s.ports.Files.List(logDir)
	if err != nil {
		return Heads{}, err
	}
	h := Heads{Streams: []Stream{}, Unreadable: []Unreadable{}, RefsRead: len(refs.Heads) + len(refs.Broken), LogsRead: len(logs)}
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
	for _, b := range refs.Broken {
		unreadable(string(b.Name), b)
	}
	for _, r := range refs.Heads {
		ref, err := ParseRef(r.Name)
		if err != nil {
			unreadable(string(r.Name), err)
			continue
		}
		if local[ref] {
			continue // read from its log, which the publication is a prefix of
		}
		// a read writes nothing, so any failure to read one stream, a
		// publication lit did not write or an object that will not read, is
		// that stream's, said, and stops no other
		pub, log, err := s.publication(r.Target)
		if err != nil {
			unreadable(string(r.Name), err)
			continue
		}
		h.Streams = append(h.Streams, Stream{Ref: ref, Until: Position{Log: log, At: pub.end}, src: pub})
	}
	slices.SortFunc(h.Streams, func(a, b Stream) int { return cmp.Compare(a.Ref.Name(), b.Ref.Name()) })
	slices.SortFunc(h.Unreadable, func(a, b Unreadable) int { return cmp.Compare(a.Name, b.Name) })
	return h, nil
}

// Scanned is one stream read whole, and the range it read.
type Scanned struct {
	Range Range
	Scan  Scan
}

// ReadAll is Heads with every stream read whole from 0, in order. A stream
// whose bytes will not read joins the heads' Unreadable, as one whose ref
// or publication will not, so which streams read is judged here alone and
// one bad blob stops no other stream.
func (s *Store) ReadAll() (Heads, []Scanned, error) {
	h, err := s.Heads()
	if err != nil {
		return Heads{}, nil, err
	}
	streams, scans := []Stream{}, []Scanned{}
	for _, st := range h.Streams {
		r := Range{Stream: st, From: 0}
		scan, err := s.Read(r)
		if err != nil {
			h.Unreadable = append(h.Unreadable, Unreadable{Name: string(st.Ref.Name()), Why: err})
			continue
		}
		streams, scans = append(streams, st), append(scans, Scanned{Range: r, Scan: scan})
	}
	h.Streams = streams
	slices.SortFunc(h.Unreadable, func(a, b Unreadable) int { return cmp.Compare(a.Name, b.Name) })
	return h, scans, nil
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
// from 0 make no publication. What makes none is NoPublication; any other
// failure is the repository's.
func (s *Store) publication(commit git.ObjectID) (published, LogID, error) {
	// a missing object or one lit did not write is no publication; a
	// failure to read is the repository's
	none := func(err error) (published, LogID, error) {
		if errors.As(err, new(git.MissingObject)) || errors.As(err, new(git.NotLit)) {
			err = NoPublication{Commit: commit, Why: err}
		}
		return published{}, LogID{}, err
	}
	refused := func(why error) (published, LogID, error) {
		return published{}, LogID{}, NoPublication{Commit: commit, Why: why}
	}
	c, err := s.ports.Objects.Commit(commit)
	if err != nil {
		return none(err)
	}
	log, err := parseCommitMessage(c.Message)
	if err != nil {
		return refused(err)
	}
	es, err := s.ports.Objects.Tree(c.Tree)
	if err != nil {
		return none(err)
	}
	pub, spans := published{segs: make([]Kept, len(es))}, make([]Span, len(es))
	for i, e := range es {
		at, err := parseSegmentName(e.Name)
		if err != nil {
			return refused(fmt.Errorf("tree %s: %w", c.Tree, err))
		}
		// the header alone, so reading heads inflates no segment
		size, err := s.ports.Objects.BlobSize(e.Blob)
		if err != nil {
			return none(err)
		}
		pub.segs[i] = Kept{Span: Span{From: at, To: at + Offset(size)}, Blob: e.Blob}
		spans[i] = pub.segs[i].Span
		pub.end += Offset(size) // the stream's end once tile has found no gap and no overlap
	}
	if err := tile(spans); err != nil {
		return refused(fmt.Errorf("tree %s: %w", c.Tree, err))
	}
	return pub, log, nil
}

// NoPublication is a commit a ref names that holds no publication: lost
// from the repository, not lit's, or segments that do not tile a stream.
// It is the one failure a publish takes as a reason to reseal; every other
// failure stops it.
type NoPublication struct {
	Commit git.ObjectID
	Why    error
}

func (n NoPublication) Error() string {
	return fmt.Sprintf("commit %s holds no publication: %v", n.Commit, n.Why)
}

func (n NoPublication) Unwrap() error { return n.Why }

// Range is a stream's bytes from From to its head.
type Range struct {
	Stream Stream
	From   Offset
}

// errPastHead is a range that starts after its stream's head.
var errPastHead = errors.New("past its head")

// Read reads a range of a stream, from From to the stream's head as Heads
// saw it, so what a fold consumes matches the heads it read.
func (s *Store) Read(r Range) (Scan, error) {
	if r.From > r.Stream.Until.At {
		return Scan{}, fmt.Errorf("%s: reading from %d, %w at %d", r.Stream.Ref.Name(), r.From, errPastHead, r.Stream.Until.At)
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
		for _, seg := range src.segs {
			sp := seg.Span
			if sp.To <= want.From || sp.From >= want.To {
				continue // outside the range read
			}
			blob, err := s.ports.Objects.Blob(seg.Blob)
			if err != nil {
				return nil, err
			}
			if uint64(len(blob)) != sp.Len() {
				return nil, fmt.Errorf("segment at %d is %d bytes; its header said %d", sp.From, len(blob), sp.Len())
			}
			b = append(b, blob[max(want.From, sp.From)-sp.From:min(want.To, sp.To)-sp.From]...)
		}
		return b, nil
	}
	panic(fmt.Sprintf("store: %T is not a source", src))
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
	kept, from, prior, was, err := s.prior(ref, st.Until)
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
	p, err := NewPublication(ref, was, st.Until.Log, segs)
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

// errOtherLog is a ref's publication of a log other than the one sealed.
var errOtherLog = errors.New("of log")

// prior is what keepWithin keeps of ref's publication of the log now at
// until, where sealing resumes, what was kept, and what the ref was when
// read, which the publish replaces from. A ref holding no publication of
// this log is resealed from 0, and the publish that follows replaces it
// (the store's decision 11).
func (s *Store) prior(ref Ref, until Position) ([]Kept, Offset, Prior, git.Was, error) {
	h, err := s.head(ref)
	if err != nil {
		return nil, 0, nil, nil, err
	}
	switch h := h.(type) {
	case git.Absent:
		return nil, 0, NoPrior{}, h, nil
	case git.Named:
		pub, log, err := s.publication(h.Target)
		switch {
		case errors.As(err, new(NoPublication)):
			return nil, 0, Resealed{Why: err}, h, nil
		case err != nil:
			return nil, 0, nil, nil, err
		case log != until.Log:
			return nil, 0, Resealed{Why: fmt.Errorf("its publication %s is %w %s, not %s", h.Target, errOtherLog, log, until.Log)}, h, nil
		case pub.end > until.At:
			return nil, 0, nil, nil, ShorterThanPublished{Ref: ref, Log: until.At, Published: pub.end}
		}
		kept, from := keepWithin(pub.segs, s.bound)
		return kept, from, KeptFrom{Commit: h.Target}, h, nil
	}
	panic(fmt.Sprintf("store: %T is not what a ref was", h))
}

// head is what ref names now.
func (s *Store) head(ref Ref) (git.Was, error) {
	l, err := s.ports.Refs.List(ref.Name())
	if err != nil {
		return nil, err
	}
	for _, h := range l.Heads {
		if h.Name == ref.Name() {
			return git.Named{Target: h.Target}, nil
		}
	}
	for _, b := range l.Broken {
		if b.Name == ref.Name() {
			return nil, b
		}
	}
	return git.Absent{}, nil
}

// RefMove is what a publish did to its ref.
//
//sumtype:decl
type RefMove interface{ move() }

// RefUnchanged is a ref that already named the commit: nothing to send.
type RefUnchanged struct{}

// RefCreated is a ref that named nothing before.
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
// segments stored as blobs in one write, one tree of every segment named
// by its start offset, one commit whose message names the log, and the ref
// replaced from what it was when p was sealed, unless it names that commit
// already. The commit is a pure function of the tree, so racing publishes
// of one log converge on one id, and a publish sealed before another
// replaced the ref is refused rather than undoing it.
func (s *Store) Publish(p Publication) (Published, error) {
	out := Published{New: []git.ObjectID{}, Prior: p.prior}
	var fresh [][]byte
	for _, seg := range p.segs {
		if f, ok := seg.(Fresh); ok {
			fresh = append(fresh, f.Bytes)
		}
	}
	puts, err := s.ports.Objects.PutBlobs(fresh)
	if err != nil {
		return Published{}, err
	}
	entries := make([]git.Entry, 0, len(p.segs))
	for _, seg := range p.segs {
		var blob git.ObjectID
		switch seg := seg.(type) {
		case Kept:
			out.Kept++
			blob = seg.Blob
		case Fresh:
			out.Sealed++
			put := puts[0]
			puts, blob = puts[1:], put.ID
			out.added(put.ID, put.Stored, len(seg.Bytes))
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
	out.Commit, out.Move = commit, moved(p.from, commit)
	// a ref already naming the commit is what this publish wanted, whoever
	// moved it there, so racing publishes of one log converge; one naming
	// anything else has moved since the seal, and this publish is stale. An
	// idle publish reads its ref, and a replace's refusal says what it is
	var now git.Was
	var moved git.Moved
	switch _, unchanged := out.Move.(RefUnchanged); {
	case unchanged:
		now, err = s.head(p.ref)
	default:
		err = s.ports.Refs.Replace(p.ref.Name(), p.from, commit)
		if err == nil {
			return out, nil
		}
		if errors.As(err, &moved) {
			now, err = moved.Now, nil
		}
	}
	if err != nil {
		return Published{}, err
	}
	if now != (git.Named{Target: commit}) {
		return Published{}, git.Moved{Name: p.ref.Name(), From: p.from, Now: now}
	}
	out.Move = RefUnchanged{}
	return out, nil
}

// moved is what pointing a ref holding h at to does to it.
func moved(h git.Was, to git.ObjectID) RefMove {
	switch h := h.(type) {
	case git.Named:
		if h.Target == to {
			return RefUnchanged{}
		}
		return RefReplaced{Previous: h.Target}
	case git.Absent:
		return RefCreated{}
	}
	panic(fmt.Sprintf("store: %T is not what a ref was", h))
}

func (out *Published) added(id git.ObjectID, stored git.Stored, bytes int) {
	switch stored {
	case git.New:
		out.New = append(out.New, id)
		out.NewBytes += uint64(bytes)
	case git.Existing:
	}
}
