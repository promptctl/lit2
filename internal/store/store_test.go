package store

import (
	"bytes"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/git/gittest"
	"github.com/promptctl/lit2/internal/panics"
)

// Ports no constructor made are no store: opening one is a bug.
func TestOpeningNoPortsIsABug(t *testing.T) {
	panics.With(t, ErrNoPorts, func() { Open(CheckoutID{0xa}, Ports{}, DefaultSegmentBound) })
}

func TestACheckoutIDHasOneSpelling(t *testing.T) {
	const s = "7a1e0f7b112d4c889e015a6b3c2f4099"
	id, err := ParseCheckoutID(s)
	if err != nil || id.String() != s {
		t.Fatalf("ParseCheckoutID(%q) = %v, %v; want it back", s, id, err)
	}
	for _, bad := range []string{"", "7A1E0F7B112D4C889E015A6B3C2F4099", s[:31], s + "00", "7a1e0f7b112d4c889e015a6b3c2f409g", s + "\n"} {
		if id, err := ParseCheckoutID(bad); !errors.Is(err, errNotACheckoutID) {
			t.Errorf("ParseCheckoutID(%q) = %v, %v; want errNotACheckoutID", bad, id, err)
		}
	}
}

func TestTheIDFileFormReadsBackWhatItWrites(t *testing.T) {
	const s = "7a1e0f7b112d4c889e015a6b3c2f4099"
	text := []byte(s + "\n")
	id, err := ReadCheckoutIDFile(text)
	if err != nil || !bytes.Equal(id.FileText(), text) {
		t.Fatalf("FileText(ReadCheckoutIDFile(%q)) = %q, %v; want it back", text, id.FileText(), err)
	}
	if back, err := ReadCheckoutIDFile(id.FileText()); err != nil || back != id {
		t.Errorf("ReadCheckoutIDFile(FileText()) = %v, %v; want %v", back, err, id)
	}
	for bad, want := range map[string]error{
		s:                                       errNotIDFileForm,
		s + "\n\n":                              errNotIDFileForm,
		s + "\r\n":                              errNotIDFileForm,
		" " + s + "\n":                          errNotIDFileForm,
		"\n":                                    errNotIDFileForm,
		s + "x":                                 errNotIDFileForm, // the form's length, no newline
		strings.Repeat("hunter2", 999):          errNotIDFileForm,
		strings.Repeat("hunter2", 4) + "zzzz\n": errNotACheckoutID,
	} {
		_, err := ReadCheckoutIDFile([]byte(bad))
		if !errors.Is(err, want) {
			t.Errorf("ReadCheckoutIDFile(%q) = %v; want %v", bad, err, want)
		}
		if err != nil && strings.Contains(err.Error(), "hunter2") {
			t.Errorf("ReadCheckoutIDFile(%q) = %v; want an error that does not carry the file", bad, err)
		}
	}
}

// forge points ref at a publication of log whose segments start at the
// offsets given and hold the bytes given, as a tree lit never cuts.
func forge(t *testing.T, p Ports, ref Ref, log LogID, at []Offset, bs [][]byte) {
	t.Helper()
	es := make([]git.Entry, len(at))
	for i := range at {
		blob, err := gittest.Put(p.objects, bs[i])
		if err != nil {
			t.Fatal(err)
		}
		es[i] = git.Entry{Name: segmentName(at[i]), Blob: blob}
	}
	tree, _, err := p.objects.PutTree(es)
	if err != nil {
		t.Fatal(err)
	}
	commit, _, err := p.objects.PutCommit(git.Commit{Tree: tree, Message: commitMessage(log)})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.refs.Replace(ref.Name(), git.Absent{}, commit); err != nil {
		t.Fatal(err)
	}
}

// A segment that does not fill its span, the last or one before it, makes
// no publication: the stream is unreadable to every reader, and its owner
// reseals it from 0.
func TestAPublicationWhoseSegmentDoesNotFillItsSpanIsUnreadableAndResealed(t *testing.T) {
	me := CheckoutID{0xa}
	s, p, frames, log := appended(t, me, "one", "two", "three")
	one, two := Offset(len(frames[0])), Offset(len(frames[0])+len(frames[1]))
	short := frames[0][:len(frames[0])-1]

	other := WriterRef{Checkout: CheckoutID{0xb}}
	forge(t, p, other, log, []Offset{0, one, two}, [][]byte{short, frames[1], frames[2]})
	h, err := s.Heads()
	if err != nil || len(h.Unreadable) != 1 || h.Unreadable[0].Name != string(other.Name()) || !errors.Is(h.Unreadable[0].Why, errMisplacedSegment) || len(h.Streams) != 1 {
		t.Errorf("with a middle segment short of its span the heads are %+v, %v; want that stream unreadable", h, err)
	}

	mine := WriterRef{Checkout: me}
	forge(t, p, mine, log, []Offset{0, one, two}, [][]byte{short, frames[1], frames[2]})
	sealed, err := s.SealOwn()
	if err != nil {
		t.Fatal(err)
	}
	pub, ok := sealed.(Publication)
	if !ok {
		t.Fatalf("sealing the own log made %#v; want a publication", sealed)
	}
	if resealed, ok := pub.prior.(Resealed); !ok || !errors.Is(resealed.Why, errMisplacedSegment) {
		t.Errorf("over its own short segment the owner seals with prior %#v; want it resealed for errMisplacedSegment", pub.prior)
	}
	for _, seg := range pub.segs {
		if _, ok := seg.(Kept); ok {
			t.Errorf("the reseal kept %#v of a publication that does not read", seg)
		}
	}
	if _, err := s.Publish(pub); err != nil {
		t.Fatal(err)
	}
	// a clone with no log, and no entropy, so a reader that drew an id
	// would fail loudly
	q := p
	q.files, q.entropy = filestest.Memory(), bytes.NewReader(nil)
	reader := Open(CheckoutID{0xc}, q, DefaultSegmentBound)
	h, err = reader.Heads()
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range h.Streams {
		if st.Ref != mine {
			continue
		}
		scan, err := reader.Read(Range{Stream: st, From: 0})
		if err != nil || len(scan.Frames) != 3 || len(scan.Torn) != 0 {
			t.Errorf("the resealed publication reads %+v, %v; want its three frames", scan, err)
		}
		return
	}
	t.Errorf("a clone with no log sees no stream of %s after the reseal: %+v", mine.Name(), h)
}

// A lit-log commit whose tree holds no segment is no publication: tile
// refuses it as it refuses a publication sealed of none, so its stream is
// unreadable to every reader and its owner reseals from 0. A publication
// of one segment, its control, reads.
func TestAPublicationOfNoSegmentIsUnreadableAndResealed(t *testing.T) {
	me := CheckoutID{0xa}
	s, p, frames, log := appended(t, me, "one")
	control, other := WriterRef{Checkout: CheckoutID{0xc}}, WriterRef{Checkout: CheckoutID{0xb}}
	forge(t, p, control, log, []Offset{0}, frames)
	forge(t, p, other, log, nil, nil)
	h, err := s.Heads()
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Unreadable) != 1 || h.Unreadable[0].Name != string(other.Name()) || !errors.Is(h.Unreadable[0].Why, errNoSegment) {
		t.Errorf("with a publication of no segment the unreadable streams are %+v; want %s, for errNoSegment", h.Unreadable, other.Name())
	}
	if !slices.ContainsFunc(h.Streams, func(st Stream) bool { return st.Ref == control }) {
		t.Errorf("control: the streams are %+v; want %s, a publication of one segment", h.Streams, control.Name())
	}

	forge(t, p, WriterRef{Checkout: me}, log, nil, nil)
	sealed, err := s.SealOwn()
	if err != nil {
		t.Fatal(err)
	}
	pub, ok := sealed.(Publication)
	if !ok {
		t.Fatalf("sealing the own log made %#v; want a publication", sealed)
	}
	if resealed, ok := pub.prior.(Resealed); !ok || !errors.Is(resealed.Why, errNoSegment) {
		t.Errorf("over its own publication of no segment the owner seals with prior %#v; want it resealed for errNoSegment", pub.prior)
	}
}

// appended is a store over fresh twins whose own log holds one frame per
// record, the frames, and the log's id.
func appended(t *testing.T, me CheckoutID, records ...string) (*Store, Ports, [][]byte, LogID) {
	t.Helper()
	p := MemoryRepositoryPorts(filestest.Memory(), bytes.NewReader(make([]byte, 64)))
	s := Open(me, p, DefaultSegmentBound)
	var frames [][]byte
	for _, r := range records {
		b, err := NewBatch(Record(r))
		if err != nil {
			t.Fatal(err)
		}
		if got, ok := s.Append(b).(Appended); !ok {
			t.Fatalf("Append = %#v; want Appended", got)
		}
		frames = append(frames, Encode(b))
	}
	log, err := s.logID(LogName(me))
	if err != nil {
		t.Fatal(err)
	}
	return s, p, frames, log
}

// A ref holding a publication of another log than the owner's is resealed
// from 0, saying it is of another log; one of the owner's log is kept from.
func TestAPublicationOfAnotherLogIsResealed(t *testing.T) {
	me := CheckoutID{0xa}
	prior := func(log func(LogID) LogID) Prior {
		t.Helper()
		s, p, frames, own := appended(t, me, "one", "two")
		forge(t, p, WriterRef{Checkout: me}, log(own), []Offset{0, Offset(len(frames[0]))}, frames)
		sealed, err := s.SealOwn()
		if err != nil {
			t.Fatal(err)
		}
		pub, ok := sealed.(Publication)
		if !ok {
			t.Fatalf("sealing the own log made %#v; want a publication", sealed)
		}
		return pub.prior
	}
	if got := prior(func(own LogID) LogID { return own }); !isKeptFrom(got) {
		t.Fatalf("over a publication of its own log the owner seals with prior %#v; want KeptFrom", got)
	}
	other := func(own LogID) LogID { own[0] ^= 0xff; return own }
	if got, ok := prior(other).(Resealed); !ok || !errors.Is(got.Why, errOtherLog) {
		t.Errorf("over a publication of another log the owner seals with prior %#v; want Resealed for errOtherLog", got)
	}
}

func isKeptFrom(p Prior) bool { _, ok := p.(KeptFrom); return ok }

func TestAReadPastItsStreamsHeadIsRefused(t *testing.T) {
	s, _, _, _ := appended(t, CheckoutID{0xa}, "one")
	h, err := s.Heads()
	if err != nil || len(h.Streams) != 1 {
		t.Fatalf("the heads are %+v, %v; want the one log's stream", h, err)
	}
	st := h.Streams[0]
	if scan, err := s.Read(Range{Stream: st, From: st.Until.At}); err != nil || len(scan.Frames) != 0 {
		t.Fatalf("a read from the head = %+v, %v; want nothing read", scan, err)
	}
	if _, err := s.Read(Range{Stream: st, From: st.Until.At + 1}); !errors.Is(err, errPastHead) {
		t.Errorf("a read from past the head = %v; want errPastHead", err)
	}
}
