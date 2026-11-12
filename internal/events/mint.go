package events

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"maps"
	"time"

	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// Frontier is the highest causal position among the events folded, the
// writer's own included: Lamport's rule mints the next event past it.
type Frontier struct{ highest uint64 }

// Observe is the frontier after folding the event h heads. The fold
// observes verified events only, so no forged position moves the clock.
func (f Frontier) Observe(h Header) Frontier { return Frontier{highest: max(f.highest, h.position.n)} }

// Names is the names created in each namespace, read-only to Mint.
type Names struct {
	taken map[Namespace]map[Segment]struct{}
}

// Taken is whether s is a created name in ns.
func (n Names) Taken(ns Namespace, s Segment) bool {
	_, ok := n.taken[ns][s]
	return ok
}

// Count is how many names ns holds.
func (n Names) Count(ns Namespace) int { return len(n.taken[ns]) }

// NamesBuilder is the fold's, fed with every created name it folds.
type NamesBuilder struct {
	taken  map[Namespace]map[Segment]struct{}
	shared bool // a Names handed out holds taken, so the next Add copies it
}

// Add records s as created in ns.
func (b *NamesBuilder) Add(ns Namespace, s Segment) {
	if b.shared || b.taken == nil {
		next := make(map[Namespace]map[Segment]struct{}, len(b.taken))
		for k, v := range b.taken {
			next[k] = maps.Clone(v)
		}
		b.taken, b.shared = next, false
	}
	if b.taken[ns] == nil {
		b.taken[ns] = map[Segment]struct{}{}
	}
	b.taken[ns][s] = struct{}{}
}

// Names is the names added so far, a value later Adds never change.
func (b *NamesBuilder) Names() Names {
	b.shared = true
	return Names{taken: b.taken}
}

// Draft is one event a command means to mint.
//
//sumtype:decl
type Draft interface{ draft() }

// Act is an event about something that exists.
type Act struct {
	About   About
	Payload Payload
}

// Create is an event that creates a name in a namespace: an item among a
// parent's children, about Item{parent.Child(name)}, or an element of an
// item, about Element{item, name}.
type Create struct {
	In      Namespace
	Payload Payload
}

func (Act) draft()    {}
func (Create) draft() {}

// Drafts is one command's drafts, in order; never empty, as store.Batch is.
type Drafts struct {
	first Draft
	rest  []Draft
}

// NewDrafts is the drafts first, then rest.
func NewDrafts(first Draft, rest ...Draft) Drafts { return Drafts{first: first, rest: rest} }

func (d Drafts) all() []Draft { return append([]Draft{d.first}, d.rest...) }

// Context is what one command mints under: its writer, what it folded and
// read, and the root's one clock reading.
type Context struct {
	Writer    store.CheckoutID
	Agent     Agent
	ConfigRef git.ObjectID
	Policy    PolicyVersion
	Frontier  Frontier
	Names     Names
	Now       time.Time
}

// Minted is a command's events and the batch that appends them, with the
// facts of their minting for the command event.
type Minted struct {
	Events []Event
	Batch  store.Batch
	// Redraws is how many minted names were taken and drawn again.
	Redraws int
}

// Mint signs ds as events at the causal positions after c's frontier,
// one each, in order: the one place a position, a name or a signature is
// made `[LAW:single-enforcer]`. A create's name is the first Width digits
// of a hash of the writer, the position and the payload hash; a name
// taken in c.Names or earlier in this command is drawn again with a new
// salt, which changes the payload hash. The name is a signed header fact,
// never a payload field, so it outlives the payload. Its error is the salt
// read failing or repeating, or the events being more than one frame of
// the log holds.
func Mint(c Context, ds Drafts, s Signer, salts io.Reader) (Minted, error) {
	if c.Now.IsZero() {
		// [LAW:no-silent-failure] the zero time is past UnixNano's range and
		// would be signed as a recorded-at in 1754; the root reads one clock
		// for every command, so a Context without it is a caller's bug
		panic("events: Mint under a Context with no clock reading")
	}
	var m Minted
	var records []store.Record
	mine := map[Namespace]map[Segment]struct{}{}
	for i, d := range ds.all() {
		position := CausalPosition{n: c.Frontier.highest + 1 + uint64(i)}
		var p Payload
		var at About
		var salt Salt
		var hash PayloadHash
		var body []byte
		draw := func() error {
			if _, err := io.ReadFull(salts, salt[:]); err != nil {
				return fmt.Errorf("drawing a payload's salt: %w", err)
			}
			hash = payloadHash(salt, body)
			return nil
		}
		switch d := d.(type) {
		case Act:
			p, at = d.Payload, d.About
			body = payloadWire(p)
			if err := draw(); err != nil {
				return Minted{}, err
			}
		case Create:
			p, body = d.Payload, payloadWire(d.Payload)
			width := Width(c.Names.Count(d.In) + len(mine[d.In]))
			for draws := 0; ; draws++ {
				if draws == maxDraws {
					// [LAW:no-silent-failure] each draw is taken with chance at
					// most 1/1024, so this many taken in a row is a salt source
					// repeating itself, not bad luck
					return Minted{}, fmt.Errorf("drawing a payload's salt: %d draws in a row named a taken name in %v, so the salt source repeats", maxDraws, d.In)
				}
				if err := draw(); err != nil {
					return Minted{}, err
				}
				name := segmentOf(nameDigest(c.Writer, position, hash), width)
				if _, again := mine[d.In][name]; !again && !c.Names.Taken(d.In, name) {
					if mine[d.In] == nil {
						mine[d.In] = map[Segment]struct{}{}
					}
					mine[d.In][name] = struct{}{}
					at = about(d.In, name)
					break
				}
				m.Redraws++
			}
		}
		h := Header{
			kind: p.Kind(), schema: p.Schema(), about: at,
			by:        Attribution{Checkout: c.Writer, Principal: s.Principal(), Agent: c.Agent},
			configRef: c.ConfigRef, policy: c.Policy, position: position,
			recordedAt: c.Now, payload: hash,
		}
		raw := h.wire()
		read, err := readHeader(raw)
		if err != nil {
			// [LAW:single-enforcer] readHeader is the one judge of a header,
			// and its error names the field a caller left unset
			panic(fmt.Sprintf("events: Mint wrote a header that does not read: %v", err))
		}
		sig := s.Sign(signed(raw))
		sigRaw := signatureWire(sig)
		m.Events = append(m.Events, Event{id: eventID(raw, sigRaw), header: read, body: Readable{Payload: p, Salt: salt}, sig: sig, raw: raw, sigRaw: sigRaw})
		records = append(records, store.Record(recordWire(raw, sigRaw, salt, body)))
	}
	b, err := store.NewBatch(records...)
	if err != nil {
		return Minted{}, fmt.Errorf("one command's events: %w", err)
	}
	m.Batch = b
	return m, nil
}

// maxDraws bounds the draws of one created name.
const maxDraws = 64

func payloadWire(p Payload) []byte {
	var w wire.Writer
	p.AppendTo(&w)
	return w.Encoded()
}

// nameDigest is the hash a created name is cut from: the design's
// (writer-id, causal-position, content), the content being the payload
// hash, so two commands of one checkout at one position mint apart.
func nameDigest(writer store.CheckoutID, p CausalPosition, hash PayloadHash) [32]byte {
	b := append([]byte("lit2 mint v1\x00"), writer[:]...)
	b = binary.BigEndian.AppendUint64(b, p.n)
	return sha256.Sum256(append(b, hash[:]...))
}

// OrderKey is an event's place in the total order: its causal position,
// then its writer, then its id. Two commands of one checkout can mint one
// (writer, position), since nothing locks a frontier, so the event id is
// the last word.
type OrderKey struct {
	Position CausalPosition
	Writer   store.CheckoutID
	ID       EventID
}

// Compare is the total order: negative when a sorts before b, zero only
// for one event.
func Compare(a, b OrderKey) int {
	switch {
	case a.Position.n < b.Position.n:
		return -1
	case a.Position.n > b.Position.n:
		return 1
	}
	if c := bytes.Compare(a.Writer[:], b.Writer[:]); c != 0 {
		return c
	}
	return bytes.Compare(a.ID[:], b.ID[:])
}

// The order key's wire fields: 1:position 2:writer 3:id.
const (
	orderPosition = 1
	orderWriter   = 2
	orderID       = 3
)

// Wire is the key as it is stored. ReadOrderKey reads it.
func (k OrderKey) Wire() []byte {
	var w wire.Writer
	w.Uvarint(orderPosition, k.Position.n)
	w.Bytes(orderWriter, k.Writer[:])
	w.Bytes(orderID, k.ID[:])
	return w.Encoded()
}

// ReadOrderKey reads a key's stored form.
func ReadOrderKey(b []byte) (OrderKey, error) {
	var k OrderKey
	var seen [orderID + 1]bool
	r := wire.NewReader(b)
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		switch tag {
		case orderPosition:
			k.Position.n, seen[tag] = r.Uvarint(), true
		case orderWriter:
			w := r.Bytes()
			seen[tag] = len(w) == len(k.Writer)
			copy(k.Writer[:], w)
		case orderID:
			id := r.Bytes()
			seen[tag] = len(id) == len(k.ID)
			copy(k.ID[:], id)
		}
		return nil
	})
	if err != nil {
		return OrderKey{}, fmt.Errorf("an order key: %w", err)
	}
	if !seen[orderPosition] || !seen[orderWriter] || !seen[orderID] || k.Position.n == 0 {
		return OrderKey{}, errors.New("an order key is a position of 1 or more, a 16-byte writer and a 32-byte id")
	}
	return k, nil
}
