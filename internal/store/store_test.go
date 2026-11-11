package store

import (
	"bytes"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/git"
)

func TestACheckoutIDHasOneSpelling(t *testing.T) {
	const s = "7a1e0f7b112d4c889e015a6b3c2f4099"
	id, err := ParseCheckoutID(s)
	if err != nil || id.String() != s {
		t.Errorf("ParseCheckoutID(%q) = %v, %v; want it back", s, id, err)
	}
	for _, bad := range []string{"", "7A1E0F7B112D4C889E015A6B3C2F4099", s[:31], s + "00", "7a1e0f7b112d4c889e015a6b3c2f409g", s + "\n"} {
		if _, err := ParseCheckoutID(bad); err == nil {
			t.Errorf("ParseCheckoutID(%q) read an id", bad)
		}
	}
}

func TestTheIDFileFormReadsBackWhatItWrites(t *testing.T) {
	const s = "7a1e0f7b112d4c889e015a6b3c2f4099"
	text := []byte(s + "\n")
	id, err := ReadCheckoutIDFile(text)
	if err != nil || !bytes.Equal(id.FileText(), text) {
		t.Errorf("FileText(ReadCheckoutIDFile(%q)) = %q, %v; want it back", text, id.FileText(), err)
	}
	if back, err := ReadCheckoutIDFile(id.FileText()); err != nil || back != id {
		t.Errorf("ReadCheckoutIDFile(FileText()) = %v, %v; want %v", back, err, id)
	}
	for _, bad := range []string{s, s + "\n\n", s + "\r\n", " " + s + "\n", "\n", strings.Repeat("hunter2", 999), strings.Repeat("hunter2", 4) + "zzzz\n"} {
		if _, err := ReadCheckoutIDFile([]byte(bad)); err == nil || strings.Contains(err.Error(), "hunter2") {
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
		blob, _, err := p.Objects.PutBlob(bs[i])
		if err != nil {
			t.Fatal(err)
		}
		es[i] = git.Entry{Name: segmentName(at[i]), Blob: blob}
	}
	tree, _, err := p.Objects.PutTree(es)
	if err != nil {
		t.Fatal(err)
	}
	commit, _, err := p.Objects.PutCommit(git.Commit{Tree: tree, Message: commitMessage(log)})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Refs.Replace(ref.Name(), commit); err != nil {
		t.Fatal(err)
	}
}

// A segment that does not fill its span, the last or one before it, makes
// no publication: the stream is unreadable to every reader, and its owner
// reseals it from 0.
func TestAPublicationWhoseSegmentDoesNotFillItsSpanIsUnreadableAndResealed(t *testing.T) {
	repo := git.NewMemory()
	p := Ports{Files: NewMemoryFiles(), Objects: repo, Refs: repo, Entropy: bytes.NewReader(make([]byte, 64))}
	me := CheckoutID{0xa}
	s := Open(me, p, DefaultSegmentBound)
	var frames [][]byte
	for _, r := range []string{"one", "two", "three"} {
		b, err := NewBatch(Record(r))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Append(b); err != nil {
			t.Fatal(err)
		}
		frames = append(frames, Encode(b))
	}
	log, err := s.logID(LogPath(me))
	if err != nil {
		t.Fatal(err)
	}
	one, two := Offset(len(frames[0])), Offset(len(frames[0])+len(frames[1]))
	short := frames[0][:len(frames[0])-1]

	other := WriterRef{Checkout: CheckoutID{0xb}}
	forge(t, p, other, log, []Offset{0, one, two}, [][]byte{short, frames[1], frames[2]})
	h, err := s.Heads()
	if err != nil || len(h.Unreadable) != 1 || h.Unreadable[0].Name != string(other.Name()) || len(h.Streams) != 1 {
		t.Errorf("with a middle segment short of its span the heads are %+v, %v; want that stream unreadable", h, err)
	}

	mine := WriterRef{Checkout: me}
	forge(t, p, mine, log, []Offset{0, one, two}, [][]byte{short, frames[1], frames[2]})
	sealed, err := s.SealOwn()
	if err != nil {
		t.Fatal(err)
	}
	pub := sealed.(Publication)
	if resealed, ok := pub.prior.(Resealed); !ok || !strings.Contains(resealed.Why.Error(), "segment") {
		t.Errorf("over its own short segment the owner seals with prior %#v; want it resealed, saying why", pub.prior)
	}
	for _, seg := range pub.segs {
		if _, ok := seg.(Kept); ok {
			t.Errorf("the reseal kept %#v of a publication that does not read", seg)
		}
	}
	if _, err := s.Publish(pub); err != nil {
		t.Fatal(err)
	}
	reader := Open(CheckoutID{0xc}, Ports{Files: NewMemoryFiles(), Objects: repo, Refs: repo, Entropy: bytes.NewReader(nil)}, DefaultSegmentBound)
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
