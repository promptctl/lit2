package events

import (
	"errors"
	"fmt"

	"github.com/promptctl/lit2/internal/events/wire"
)

// Kind is an event's type, such as "item.create": an open set, since a
// newer binary writes kinds this one has never heard of.
type Kind string

// SchemaVersion is a kind's payload version, 1 and up; it is raised only
// in a release (schema-reversibility.md).
type SchemaVersion uint32

// Versioned is a payload at some version of its kind.
type Versioned interface {
	Kind() Kind
	Schema() SchemaVersion
}

// Payload is a kind's current version, the one Mint writes. Every payload
// keeps the fields it does not know and writes them back, so one an older
// binary decodes, changes and encodes keeps a newer binary's fields.
type Payload interface {
	Versioned
	AppendTo(w *wire.Writer)
}

// Past is a frozen version of a kind. Up is its one upcaster, to the next
// version; a schema bump adds one decoder, one Past and one upcaster and
// touches no frozen one (event-store design §events).
type Past interface {
	Versioned
	Up() Versioned
}

// KindRow is one kind's decoders, the registry's row; one registry serves
// decoding and folding.
type KindRow struct {
	kind    Kind
	current SchemaVersion
	decode  []func(*wire.Reader) (Versioned, error)
}

// errRowVersion is a row whose decoders do not reach its kind's version.
var errRowVersion = errors.New("a row whose decoders do not reach its kind's version")

// Row is P's row: decode[i] reads version i+1, so the last reads P's own
// version. The row's kind is P's, so the two cannot disagree
// `[LAW:one-source-of-truth]`; a row whose decoders do not reach P's
// version is a build fault.
func Row[P Payload](decode ...func(*wire.Reader) (Versioned, error)) KindRow {
	var p P
	if int(p.Schema()) != len(decode) {
		panic(fmt.Errorf("events: %s is at version %d with %d decoders: %w", p.Kind(), p.Schema(), len(decode), errRowVersion))
	}
	return KindRow{kind: p.Kind(), current: p.Schema(), decode: decode}
}

// Kind is the row's kind.
func (r KindRow) Kind() Kind { return r.kind }

// Kinds is the decode table: every kind this binary reads. It is built
// once at the root and never changed.
type Kinds struct{ rows map[Kind]KindRow }

// NewKinds is the table of rows; two rows of one kind is a build fault.
func NewKinds(rows ...KindRow) Kinds {
	by := map[Kind]KindRow{}
	for _, r := range rows {
		if _, ok := by[r.kind]; ok {
			panic(fmt.Errorf("events: kind %s: %w", r.kind, errRegisteredTwice))
		}
		by[r.kind] = r
	}
	return Kinds{rows: by}
}

// read decodes a committed payload of kind k at version v, salted with
// salt, and upcasts it to the current version. A failure to read is never
// absence: each one is the UnreadWhy it is.
func (ks Kinds) read(k Kind, v SchemaVersion, salt Salt, payload []byte) Body {
	row, ok := ks.rows[k]
	switch {
	case !ok:
		return Unread{Why: UnknownKind{}}
	case v > row.current:
		return Unread{Why: NewerSchema{Have: v, Max: row.current}}
	}
	at, err := row.decode[v-1](wire.NewReader(payload))
	if err != nil {
		return Unread{Why: Undecodable{Err: err}}
	}
	// each step is one upcaster, so the chain is at most current-v long
	for range row.current - v {
		past, ok := at.(Past)
		if !ok {
			break
		}
		at = past.Up()
	}
	p, ok := at.(Payload)
	if !ok || p.Kind() != k || p.Schema() != row.current {
		return Unread{Why: Undecodable{Err: fmt.Errorf("version %d of %s reads as %T, not %s's version %d payload", v, k, at, k, row.current)}}
	}
	return Readable{Payload: p, Salt: salt}
}
