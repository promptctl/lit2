package events

import (
	"bytes"
	"fmt"
	"io"
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

// Draft is one event a command means to mint.
//
//sumtype:decl
type Draft interface{ draft() }

// Act is an event about something that exists. A create replayed from
// another log is an Act about its NewItem or NewElement, so it keeps its
// id and its segment (§3.2).
type Act struct {
	About   About
	Payload Payload
}

// CreateItem is an item.create: Mint names the item, in the namespace its
// placement puts it in.
type CreateItem struct{ Payload ItemCreate }

// CreateElement is a create of an element of item Of, such as a
// comment.add: Mint names the element among Of's elements.
type CreateElement struct {
	Of      ItemID
	Payload Payload
}

func (Act) draft()           {}
func (CreateItem) draft()    {}
func (CreateElement) draft() {}

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
	Known     Known
	Now       time.Time
}

// Minted is a command's events and the batch that appends them.
type Minted struct {
	Events []Event
	Batch  store.Batch
}

// Mint signs ds as events at the causal positions after c's frontier,
// one each, in order: the one place a position, an id or a signature is
// made `[LAW:single-enforcer]`. A create's id is 128 bits read from
// entropy, the process's source (D6), and its segment is as wide as its
// namespace's fill needs and wider than any known id's shared digits, so it
// is never a segment already fixed: a later mint goes wider (§3.2). The id
// and the width are signed header facts, never payload fields, so they
// outlive the payload. Its error is entropy failing or repeating, or the
// events being more than one frame of the log holds.
func Mint(c Context, ds Drafts, s Signer, entropy io.Reader) (Minted, error) {
	if c.Now.IsZero() {
		// [LAW:no-silent-failure] the zero time is past UnixNano's range and
		// would be signed as a recorded-at in 1754; the root reads one clock
		// for every command, so a Context without it is a caller's bug
		panic("events: Mint under a Context with no clock reading")
	}
	var m Minted
	var records []store.Record
	var mine [][16]byte
	fill := map[Namespace]int{}
	name := func(in Namespace) ([16]byte, int, error) {
		var id [16]byte
		if _, err := io.ReadFull(entropy, id[:]); err != nil {
			return id, 0, fmt.Errorf("drawing an id: %w", err)
		}
		shared := c.Known.shared(id)
		for _, other := range mine {
			shared = max(shared, sharedDigits(id, other))
		}
		if shared == fullWidth {
			// [LAW:no-silent-failure] 128 random bits repeat only when the
			// source does
			return id, 0, fmt.Errorf("drawing an id: %s is already known, so the entropy source repeats", digits(id, fullWidth))
		}
		w := max(Width(c.Known.Fill(in)+fill[in]), shared+1)
		mine, fill[in] = append(mine, id), fill[in]+1
		return id, w, nil
	}
	for i, d := range ds.all() {
		position := CausalPosition{n: c.Frontier.highest + 1 + uint64(i)}
		var p Payload
		var at About
		switch d := d.(type) {
		case Act:
			p, at = d.Payload, d.About
		case CreateItem:
			id, w, err := name(namespaceOf(d.Payload.Place))
			if err != nil {
				return Minted{}, err
			}
			p, at = d.Payload, NewItem{ID: ItemID{b: id}, Width: w}
		case CreateElement:
			id, w, err := name(ElementOf{Item: d.Of})
			if err != nil {
				return Minted{}, err
			}
			p, at = d.Payload, NewElement{Of: d.Of, ID: ElementID{b: id}, Width: w}
		}
		body := payloadWire(p)
		var salt Salt
		if _, err := io.ReadFull(entropy, salt[:]); err != nil {
			return Minted{}, fmt.Errorf("drawing a payload's salt: %w", err)
		}
		h := Header{
			kind: p.Kind(), schema: p.Schema(), about: at,
			by:        Attribution{Checkout: c.Writer, Principal: s.Principal(), Agent: c.Agent},
			configRef: c.ConfigRef, policy: c.Policy, position: position,
			recordedAt: c.Now, payload: payloadHash(salt, body),
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

// namespaceOf is the namespace an item placed at p is named in.
func namespaceOf(p Placement) Namespace {
	switch p := p.(type) {
	case AtLevel:
		return Top{}
	case BelowParent:
		return ChildOf{Parent: p.Parent}
	}
	panic(fmt.Sprintf("events: %T is not a Placement", p))
}

func payloadWire(p Payload) []byte {
	var w wire.Writer
	p.AppendTo(&w)
	return w.Encoded()
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
