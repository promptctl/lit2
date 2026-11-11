package events

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// The test kinds. This binary's table, here, knows note and made; ahead,
// the table of a binary one release on, knows one more note field and one
// more kind; later knows note at version 2 and reads version 1 through its
// one upcaster.

// note is test.note at version 1: an envelope and a list of tags.
type note struct {
	Text    Envelope
	Tags    []string
	unknown wire.Unknown
}

func (note) Kind() Kind            { return "test.note" }
func (note) Schema() SchemaVersion { return 1 }

func (n note) appendTo(w *wire.Writer) {
	w.Bytes(1, n.Text.wire())
	for _, t := range n.Tags {
		w.Bytes(2, []byte(t))
	}
	w.Keep(n.unknown)
}

func readNote(r *wire.Reader) (Versioned, error) {
	var n note
	var err error
	for tag, ok := r.Next(); ok && err == nil; tag, ok = r.Next() {
		switch tag {
		case 1:
			n.Text, err = readEnvelope(r.Bytes())
		case 2:
			n.Tags = append(n.Tags, string(r.Bytes()))
		}
	}
	n.unknown = r.Rest()
	return n, errors.Join(err, r.Err())
}

// newerNote is test.note as the newer binary has it: one more field.
type newerNote struct {
	Text    Envelope
	Tags    []string
	Mood    string
	unknown wire.Unknown
}

func (newerNote) Kind() Kind            { return "test.note" }
func (newerNote) Schema() SchemaVersion { return 1 }

func (n newerNote) appendTo(w *wire.Writer) {
	w.Bytes(1, n.Text.wire())
	for _, t := range n.Tags {
		w.Bytes(2, []byte(t))
	}
	w.Bytes(3, []byte(n.Mood))
	w.Keep(n.unknown)
}

func readNewerNote(r *wire.Reader) (Versioned, error) {
	var n newerNote
	var err error
	for tag, ok := r.Next(); ok && err == nil; tag, ok = r.Next() {
		switch tag {
		case 1:
			n.Text, err = readEnvelope(r.Bytes())
		case 2:
			n.Tags = append(n.Tags, string(r.Bytes()))
		case 3:
			n.Mood = string(r.Bytes())
		}
	}
	n.unknown = r.Rest()
	return n, errors.Join(err, r.Err())
}

// made is test.made, a kind that creates: an item or an element.
type made struct{ Title Envelope }

func (made) Kind() Kind                { return "test.made" }
func (made) Schema() SchemaVersion     { return 1 }
func (m made) appendTo(w *wire.Writer) { w.Bytes(1, m.Title.wire()) }

func readMade(r *wire.Reader) (Versioned, error) {
	var m made
	var err error
	for tag, ok := r.Next(); ok && err == nil; tag, ok = r.Next() {
		if tag == 1 {
			m.Title, err = readEnvelope(r.Bytes())
		}
	}
	return m, errors.Join(err, r.Err())
}

// newer is test.newer, a kind only the newer binary knows.
type newer struct{}

func (newer) Kind() Kind                        { return "test.newer" }
func (newer) Schema() SchemaVersion             { return 1 }
func (newer) appendTo(*wire.Writer)             {}
func readNewer(*wire.Reader) (Versioned, error) { return newer{}, nil }

// pastNote is test.note's version 1 as the later binary keeps it, frozen.
type pastNote struct{ Text Envelope }

func (pastNote) Kind() Kind            { return "test.note" }
func (pastNote) Schema() SchemaVersion { return 1 }
func (p pastNote) Up() Versioned       { return laterNote{Text: p.Text, Pinned: false} }

func readPastNote(r *wire.Reader) (Versioned, error) {
	var p pastNote
	var err error
	for tag, ok := r.Next(); ok && err == nil; tag, ok = r.Next() {
		if tag == 1 {
			p.Text, err = readEnvelope(r.Bytes())
		}
	}
	return p, errors.Join(err, r.Err())
}

// laterNote is test.note at version 2.
type laterNote struct {
	Text   Envelope
	Pinned bool
}

func (laterNote) Kind() Kind            { return "test.note" }
func (laterNote) Schema() SchemaVersion { return 2 }

func (n laterNote) appendTo(w *wire.Writer) {
	w.Bytes(1, n.Text.wire())
	if n.Pinned {
		w.Uvarint(3, 1)
	}
}

func readLaterNote(r *wire.Reader) (Versioned, error) {
	var n laterNote
	var err error
	for tag, ok := r.Next(); ok && err == nil; tag, ok = r.Next() {
		switch tag {
		case 1:
			n.Text, err = readEnvelope(r.Bytes())
		case 3:
			n.Pinned = r.Uvarint() == 1
		}
	}
	return n, errors.Join(err, r.Err())
}

// garbled is a broken binary's test.note: bytes no note decoder reads.
type garbled struct{}

func (garbled) Kind() Kind              { return "test.note" }
func (garbled) Schema() SchemaVersion   { return 1 }
func (garbled) appendTo(w *wire.Writer) { w.Uvarint(1, 7) }

var (
	here  = NewKinds(Row[note](readNote), Row[made](readMade))
	ahead = NewKinds(Row[newerNote](readNewerNote), Row[made](readMade), Row[newer](readNewer))
	later = NewKinds(Row[laterNote](readPastNote, readLaterNote), Row[made](readMade))

	suites = NewSuites(Ed25519())
	prefix = mustPrefix("lit")
	config = mustConfigRef("2e65efe2a145dda7ee51d1741299f848e5bf752e")
	t0     = time.Date(2026, time.November, 11, 12, 0, 0, 0, time.UTC)
)

func mustPrefix(s string) Prefix {
	p, err := ParsePrefix(s)
	if err != nil {
		panic(err)
	}
	return p
}

func mustConfigRef(s string) git.ObjectID {
	id, err := git.ParseObjectID(git.SHA1, s)
	if err != nil {
		panic(err)
	}
	return id
}

// keyOf is a key drawn from a seed, so each test names its principals.
func keyOf(seed byte) Key {
	k, err := NewKey(bytes.NewReader(bytes.Repeat([]byte{seed}, 32)))
	if err != nil {
		panic(err)
	}
	return k
}

// writer is a checkout id named by one byte.
func writer(b byte) store.CheckoutID { return store.CheckoutID{b} }

// context is a context for writer w at frontier f with the names n.
func context(w byte, f Frontier, n Names) Context {
	return Context{Writer: writer(w), Agent: Unlabeled{}, ConfigRef: config, Policy: 1, Frontier: f, Names: n, Now: t0}
}

// salts is a seeded source of salts.
func salts(seed uint64) *rand.ChaCha8 {
	var s [32]byte
	s[0] = byte(seed)
	s[1] = byte(seed >> 8)
	return rand.NewChaCha8(s)
}

func mint(t testing.TB, c Context, k Key, seed uint64, first Draft, rest ...Draft) Minted {
	t.Helper()
	m, err := Mint(c, NewDrafts(first, rest...), k.Signer(), salts(seed))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// decoded is the event record r decodes to under ks, failing the test on a
// Malformed record.
func decoded(t testing.TB, ks Kinds, r store.Record) Event {
	t.Helper()
	switch d := Decode(ks, r).(type) {
	case Event:
		return d
	case Malformed:
		t.Fatalf("a record decodes Malformed: %s", d.Why)
	}
	panic("unreachable")
}

func records(m Minted) []store.Record { return m.Batch.Records() }

func text(s string) Envelope { return SealPlain(s) }

func words(r *rand.Rand, n int) string {
	ws := make([]string, n)
	for i := range ws {
		ws[i] = english[r.IntN(len(english))]
	}
	return strings.Join(ws, " ")
}
