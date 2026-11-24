package events

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"time"

	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// Event is one signed record of the log, as read or as minted. It is
// opaque: only Decode and Mint build one, from its stored bytes, so its
// header, id and signature cannot disagree with them
// `[LAW:types-are-the-program]`.
type Event struct {
	id     EventID
	header Header
	body   Body
	sig    Signature
	raw    []byte // the header as stored, which the signature covers
	sigRaw []byte // the signature as stored, which the id hashes with raw
}

// ID is the event's identity: a hash of its header bytes and its signature,
// so a payload rewrite leaves it unchanged, and under strict Ed25519,
// which admits no second valid signature, a valid event has one id.
func (e Event) ID() EventID { return e.id }

// Header is what the event's signature covers.
func (e Event) Header() Header { return e.header }

// Body is the payload as this binary reads it.
func (e Event) Body() Body { return e.body }

// Signature is the event's signature over SignedBytes.
func (e Event) Signature() Signature { return e.sig }

// SignedBytes is what the signature signs: a domain tag and the header as
// stored, never re-encoded, so a newer writer's header fields are signed
// and kept.
func (e Event) SignedBytes() []byte { return signed(e.raw) }

// Order is the event's place in the total order.
func (e Event) Order() OrderKey {
	return OrderKey{Position: e.header.position, Writer: e.header.by.Checkout, ID: e.id}
}

func signed(header []byte) []byte { return append([]byte("lit2 header v1\x00"), header...) }

// eventID hashes the header and the signature, the header's length first
// so no two pairs concatenate alike.
func eventID(header, sig []byte) EventID {
	h := sha256.New()
	h.Write([]byte("lit2 event v1\x00"))
	h.Write(binary.AppendUvarint(nil, uint64(len(header))))
	h.Write(header)
	h.Write(sig)
	return EventID(h.Sum(nil))
}

// EventID names one event.
type EventID [32]byte

func (id EventID) String() string { return hex.EncodeToString(id[:]) }

// FileText is the id in a founding file's form, which ReadEventIDFile
// reads: its 64 lowercase hex digits and one newline.
func (id EventID) FileText() []byte { return []byte(id.String() + "\n") }

// errNotAnEventID is a founding file's text that is no event id.
var errNotAnEventID = errors.New("an event id file's text that is no event id")

// ReadEventIDFile reads a founding file: its 64 lowercase hex digits, and
// the one line ending, LF or CRLF, or none, that a git checkout or an
// editor leaves on a committed file. It is FileText's inverse, and no
// error carries the file's bytes.
func ReadEventIDFile(b []byte) (EventID, error) {
	text, crlf := bytes.CutSuffix(b, []byte("\r\n"))
	if !crlf {
		text, _ = bytes.CutSuffix(b, []byte("\n"))
	}
	if err := sized(partEventIDText, text, 2*len(EventID{})); err != nil {
		return EventID{}, err
	}
	var id EventID
	if n, err := hex.Decode(id[:], text); err != nil || n != len(id) || hex.EncodeToString(id[:]) != string(text) {
		return EventID{}, fmt.Errorf("an event id is 64 lowercase hex digits: %w", errNotAnEventID)
	}
	return id, nil
}

// ReadEventID reads an id's stored form.
func ReadEventID(b []byte) (EventID, error) {
	if err := sized(partEventID, b, len(EventID{})); err != nil {
		return EventID{}, err
	}
	return EventID(b), nil
}

// CausalPosition is an event's Lamport timestamp, 1 and up. Only the header
// reader and Mint make one, so a position is minted in Mint alone and never
// forged by a caller.
type CausalPosition struct{ n uint64 }

// PolicyVersion is the policy version an event cites, 1 and up; the
// enrollment cites 1, the version it founds.
type PolicyVersion uint64

// PayloadHash commits the header to the payload: a hash of the salt and
// the payload's bytes.
type PayloadHash [32]byte

// Salt is the 32 random bytes the payload hash mixes in, so a low-entropy
// payload cannot be recovered by guessing its hash. It travels with the
// payload and is erased with it.
type Salt [32]byte

func payloadHash(s Salt, payload []byte) PayloadHash {
	h := sha256.New()
	h.Write([]byte("lit2 payload v1\x00"))
	h.Write(s[:])
	h.Write(payload)
	return PayloadHash(h.Sum(nil))
}

// Attribution is who wrote an event (§3.14).
type Attribution struct {
	Checkout  store.CheckoutID
	Principal PrincipalID
	Agent     Agent
}

// Header is the signed part of an event: a read-only view of its stored
// bytes. Fields this binary does not know stay in those bytes, signed,
// and are never re-encoded.
type Header struct {
	kind       Kind
	schema     SchemaVersion
	about      About
	by         Attribution
	configRef  git.ObjectID
	policy     PolicyVersion
	position   CausalPosition
	recordedAt time.Time
	payload    PayloadHash
}

// Kind is the event's type.
func (h Header) Kind() Kind { return h.kind }

// Schema is the payload's version of its kind.
func (h Header) Schema() SchemaVersion { return h.schema }

// About is what the event is about; a create's is what it creates.
func (h Header) About() About { return h.about }

// Subject is the node the event concerns.
func (h Header) Subject() Subject { return subjectOf(h.about) }

// Attribution is who wrote the event.
func (h Header) Attribution() Attribution { return h.by }

// ConfigRef is the code repository's blob of .lit/workspace.toml the writer
// read: provenance, never read by the fold (decision H).
func (h Header) ConfigRef() git.ObjectID { return h.configRef }

// Policy is the policy version the event cites.
func (h Header) Policy() PolicyVersion { return h.policy }

// Position is the event's causal position.
func (h Header) Position() CausalPosition { return h.position }

// RecordedAt is the writer's clock when it minted the event, in UTC: data,
// never order (INV:wall-time-is-data).
func (h Header) RecordedAt() time.Time { return h.recordedAt }

// PayloadHash is the hash the header commits the payload to.
func (h Header) PayloadHash() PayloadHash { return h.payload }

// Body is the payload as this binary reads it.
//
//sumtype:decl
type Body interface{ body() }

// Readable is a committed payload, decoded and upcast to the current
// version, with its salt.
type Readable struct {
	Payload Payload
	Salt    Salt
}

// Unread is a committed payload this binary cannot read; the header is
// whole, so the event is carried and named, never dropped
// (INV:preserve-unknown).
type Unread struct{ Why UnreadWhy }

// Erased is an event whose body a rewrite dropped.
type Erased struct{}

// Uncommitted is a body the header's payload hash does not commit to: a
// rewrite replaced it.
type Uncommitted struct{}

func (Readable) body()    {}
func (Unread) body()      {}
func (Erased) body()      {}
func (Uncommitted) body() {}

// UnreadWhy is why a committed payload is unread.
//
//sumtype:decl
type UnreadWhy interface{ unreadWhy() }

// UnknownKind is a kind this binary has no row for.
type UnknownKind struct{}

// NewerSchema is a version past the newest this binary reads.
type NewerSchema struct{ Have, Max SchemaVersion }

// Undecodable is a committed payload its kind's decoder refused.
type Undecodable struct{ Err error }

func (UnknownKind) unreadWhy() {}
func (NewerSchema) unreadWhy() {}
func (Undecodable) unreadWhy() {}

// Decoded is what one record of the log is: an event, or a record whose
// header cannot be read.
//
//sumtype:decl
type Decoded interface{ decoded() }

// Malformed is a record with no readable header, named by its digest.
type Malformed struct {
	Why    string
	Digest [32]byte
}

func (Event) decoded()     {}
func (Malformed) decoded() {}

// The record's wire fields: record = 1:header · 2:signature{1:suite 2:bytes}
// · 3:body{1:salt 2:payload}, absent when erased · unknown….
const (
	recordHeader    = 1
	recordSignature = 2
	recordBody      = 3
)

// The header's wire fields: header = 1:kind 2:schema 3:subject{1:node
// 2:element name} 4:attribution{1:checkout 2:principal{1:suite 2:key}
// 3:agent} 5:config-ref{1:format 2:bytes} 6:policy 7:position
// 8:recorded-at (unix ns) 9:payload-hash · unknown….
const (
	headerKind = iota + 1
	headerSchema
	headerSubject
	headerAttribution
	headerConfigRef
	headerPolicy
	headerPosition
	headerRecordedAt
	headerPayloadHash
)

// Decode reads one record of the log `[LAW:parse-dont-validate]`. A record
// whose header cannot be read is Malformed; every other record is an
// Event, whose body says what this binary made of its payload.
func Decode(k Kinds, r store.Record) Decoded {
	rec, err := readRecord(r)
	if err != nil {
		return Malformed{Why: err.Error(), Digest: sha256.Sum256(r)}
	}
	h, err := readHeader(rec.header)
	if err != nil {
		return Malformed{Why: err.Error(), Digest: sha256.Sum256(r)}
	}
	return Event{id: eventID(rec.header, rec.sigRaw), header: h, body: bodyOf(k, h, rec), sig: rec.sig, raw: rec.header, sigRaw: rec.sigRaw}
}

// record is a record's parts as stored.
type record struct {
	header, sigRaw []byte
	sig            Signature
	body           []byte
	hasBody        bool
}

func readRecord(b []byte) (record, error) {
	var rec record
	r := wire.NewReader(b)
	seen, err := readOnce(r, map[uint32]part{recordHeader: partHeader, recordSignature: partSignature, recordBody: partBody}, func(tag uint32, r *wire.Reader) error {
		switch tag {
		case recordHeader:
			rec.header = bytes.Clone(r.Bytes())
		case recordSignature:
			rec.sigRaw = bytes.Clone(r.Bytes())
		case recordBody:
			rec.body, rec.hasBody = bytes.Clone(r.Bytes()), true
		}
		return nil
	})
	switch {
	case err != nil:
		return record{}, fmt.Errorf("the record: %w", err)
	case !seen[recordHeader]:
		return record{}, fmt.Errorf("the record: %w", missing{What: partHeader})
	case !seen[recordSignature]:
		return record{}, fmt.Errorf("the record: %w", missing{What: partSignature})
	}
	sig, err := readSignature(rec.sigRaw)
	if err != nil {
		return record{}, err
	}
	rec.sig = sig
	return rec, nil
}

func readSignature(b []byte) (Signature, error) {
	var s Signature
	r := wire.NewReader(b)
	seen, err := readOnce(r, map[uint32]part{1: partSuite, 2: partSignatureBytes}, func(tag uint32, r *wire.Reader) error {
		switch tag {
		case 1:
			suite, err := byteOf(r)
			if err != nil {
				return err
			}
			s.Suite = SuiteID(suite)
		case 2:
			s.Bytes = bytes.Clone(r.Bytes())
		}
		return nil
	})
	switch {
	case err != nil:
		return Signature{}, fmt.Errorf("the signature: %w", err)
	case !seen[1]:
		return Signature{}, fmt.Errorf("the signature: %w", missing{What: partSuite})
	}
	return s, nil
}

// bodyOf is what this binary makes of rec's body under h.
func bodyOf(k Kinds, h Header, rec record) Body {
	if !rec.hasBody {
		return Erased{}
	}
	var salt Salt
	var payload []byte
	r := wire.NewReader(rec.body)
	seen, err := readOnce(r, map[uint32]part{1: partSalt, 2: partPayload}, func(tag uint32, r *wire.Reader) error {
		switch tag {
		case 1:
			b := r.Bytes()
			copy(salt[:], b)
			return sized(partSalt, b, len(salt))
		case 2:
			payload = bytes.Clone(r.Bytes())
		}
		return nil
	})
	// a body that does not parse, gives a field twice, or whose hash is not
	// the header's, is not the body the writer signed
	if err != nil || !seen[1] || !seen[2] || payloadHash(salt, payload) != h.payload {
		return Uncommitted{}
	}
	return k.read(h.kind, h.schema, salt, payload)
}

// The writers' preconditions: an About, an Agent and a segment width are
// sum types and a range Go lets a caller leave outside, and a Labeled agent a
// label Go lets a caller leave empty.
var (
	errNotAnAbout = errors.New("a type that is no About")
	errNotAnAgent = errors.New("a type that is no Agent")
	errWidth      = errors.New("a segment width outside an id's")
	errEmptyLabel = errors.New("a Labeled agent with no label")
)

// wire is the header as Mint writes it, which readHeader reads.
func (h Header) wire() []byte {
	var w wire.Writer
	w.Bytes(headerKind, []byte(h.kind))
	w.Uvarint(headerSchema, uint64(h.schema))
	w.Bytes(headerSubject, aboutWire(h.about))
	w.Bytes(headerAttribution, h.by.wire())
	w.Bytes(headerConfigRef, objectIDWire(h.configRef, "config-ref"))
	w.Uvarint(headerPolicy, uint64(h.policy))
	w.Uvarint(headerPosition, h.position.n)
	w.Uvarint(headerRecordedAt, uint64(h.recordedAt.UnixNano()))
	w.Bytes(headerPayloadHash, h.payload[:])
	return w.Encoded()
}

// aboutWire is a subject as it is stored. readAbout reads it.
func aboutWire(a About) []byte {
	var w wire.Writer
	switch a := a.(type) {
	case Root:
	case Item:
		w.Bytes(subjectItem, a.ID.b[:])
	case NewItem:
		w.Bytes(subjectItem, a.ID.b[:])
		w.Uvarint(subjectWidth, widthWire(a.Width))
	case Element:
		w.Bytes(subjectItem, a.Of.b[:])
		w.Bytes(subjectElement, a.ID.b[:])
	case NewElement:
		w.Bytes(subjectItem, a.Of.b[:])
		w.Bytes(subjectElement, a.ID.b[:])
		w.Uvarint(subjectWidth, widthWire(a.Width))
	default:
		// [LAW:no-silent-failure] an empty subject reads as the root, so a
		// draft naming nothing would mint a signed event about the root
		panic(fmt.Errorf("events: %T: %w", a, errNotAnAbout))
	}
	return w.Encoded()
}

// The subject's wire fields: 1:item id 2:element id 3:segment width.
const (
	subjectItem    = 1
	subjectElement = 2
	subjectWidth   = 3
)

// widthWire is a segment width as it is stored. Go lets a caller write any
// int, and a width outside 3..26 is no segment of an id, so writing one is
// a caller's bug.
func widthWire(w int) uint64 {
	if w < minWidth || w > fullWidth {
		panic(fmt.Errorf("events: a segment width of %d, not %d to %d: %w", w, minWidth, fullWidth, errWidth))
	}
	return uint64(w)
}

// Wire is the attribution as it is stored. readAttribution reads it.
func (a Attribution) wire() []byte {
	var w wire.Writer
	w.Bytes(1, a.Checkout[:])
	w.Bytes(2, a.Principal.wire())
	switch agent := a.Agent.(type) {
	case Labeled:
		// [LAW:single-enforcer] an empty label reads back as Unlabeled, so a
		// zero Labeled, which Go lets a caller write, would sign another claim
		if agent.label == "" {
			panic(fmt.Errorf("events: an empty label reads back as Unlabeled: %w", errEmptyLabel))
		}
		w.Bytes(3, []byte(agent.label))
	case Unlabeled:
	default:
		// [LAW:no-silent-failure] no label reads back as Unlabeled, so a
		// Context naming no agent would sign a claim it never made
		panic(fmt.Errorf("events: %T: %w", a.Agent, errNotAnAgent))
	}
	return w.Encoded()
}

// objectIDWire is a git object id as an event stores it, its format and
// bytes: the header's config-ref and an applied version's blob. what names
// the field for the panic.
func objectIDWire(id git.ObjectID, what string) []byte {
	if id.Format() == 0 {
		// [LAW:types-are-the-program] exception: git.ObjectID's zero value
		// names no object, yet every field written here names one; the
		// writer holds a parsed or hashed id, so this is a caller's bug.
		panic("events: a " + what + " that names no object")
	}
	var w wire.Writer
	w.Uvarint(1, uint64(id.Format()))
	w.Bytes(2, id.Sum())
	return w.Encoded()
}

func signatureWire(s Signature) []byte {
	var w wire.Writer
	w.Uvarint(1, uint64(s.Suite))
	w.Bytes(2, s.Bytes)
	return w.Encoded()
}

func recordWire(header, sig []byte, salt Salt, payload []byte) []byte {
	var body, w wire.Writer
	body.Bytes(1, salt[:])
	body.Bytes(2, payload)
	w.Bytes(recordHeader, header)
	w.Bytes(recordSignature, sig)
	w.Bytes(recordBody, body.Encoded())
	return w.Encoded()
}

// headerParts is each header field by its tag.
var headerParts = map[uint32]part{headerKind: partKind, headerSchema: partSchema, headerSubject: partSubject,
	headerAttribution: partAttribution, headerConfigRef: partConfigRef, headerPolicy: partPolicy,
	headerPosition: partPosition, headerRecordedAt: partRecordedAt, headerPayloadHash: partPayloadHash}

// errNotATime is a recorded-at outside the clock's range.
var errNotATime = errors.New("a recorded-at no clock past 1970 reads as a time")

// readHeader reads a header's stored bytes. A header missing a field the
// event cannot be without, or a root carrying an element name, is refused.
func readHeader(b []byte) (Header, error) {
	var h Header
	r := wire.NewReader(b)
	var err error
	seen, err := readOnce(r, headerParts, func(tag uint32, r *wire.Reader) error {
		switch tag {
		case headerKind:
			h.kind = Kind(r.Bytes())
		case headerSchema:
			ver := r.Uvarint()
			if ver > math.MaxUint32 {
				err = fmt.Errorf("the schema version is %d, which no SchemaVersion (uint32) holds", ver)
			}
			h.schema = SchemaVersion(ver)
		case headerSubject:
			h.about, err = readAbout(r.Bytes())
		case headerAttribution:
			h.by, err = readAttribution(r.Bytes())
		case headerConfigRef:
			h.configRef, err = readObjectID(r.Bytes(), "the config-ref")
		case headerPolicy:
			h.policy = PolicyVersion(r.Uvarint())
		case headerPosition:
			h.position = CausalPosition{n: r.Uvarint()}
		case headerRecordedAt:
			ns := r.Uvarint()
			if ns == 0 || ns > math.MaxInt64 {
				err = fmt.Errorf("the recorded-at is %d ns: %w", ns, errNotATime)
			}
			h.recordedAt = time.Unix(0, int64(ns)).UTC()
		case headerPayloadHash:
			c := r.Bytes()
			copy(h.payload[:], c)
			err = sized(partPayloadHash, c, len(h.payload))
		}
		return err
	})
	if err != nil {
		return Header{}, fmt.Errorf("the header: %w", err)
	}
	for _, tag := range slices.Sorted(maps.Keys(headerParts)) {
		if !seen[tag] {
			return Header{}, fmt.Errorf("the header: %w", missing{What: headerParts[tag]})
		}
	}
	switch {
	case h.kind == "":
		return Header{}, fmt.Errorf("the header: %w", zeroField{What: partKind})
	case h.schema == 0:
		return Header{}, fmt.Errorf("the header: %w", zeroField{What: partSchema})
	case h.policy == 0:
		return Header{}, fmt.Errorf("the header: %w", zeroField{What: partPolicy})
	case h.position.n == 0:
		return Header{}, fmt.Errorf("the header: %w", zeroField{What: partPosition})
	}
	return h, nil
}

// errRootNamesPart is a subject of the root that names an element or a
// segment, which the root has none of.
var errRootNamesPart = errors.New("a root subject naming an element or a segment, which the root has none of")

// readAbout reads a subject's stored form: nothing, an item, an item and
// an element, either with a segment width.
func readAbout(b []byte) (About, error) {
	var item, element []byte
	var width uint64
	r := wire.NewReader(b)
	seen, err := readOnce(r, map[uint32]part{subjectItem: partItem, subjectElement: partElement, subjectWidth: partWidth}, func(tag uint32, r *wire.Reader) error {
		switch tag {
		case subjectItem:
			item = r.Bytes()
		case subjectElement:
			element = r.Bytes()
		case subjectWidth:
			width = r.Uvarint()
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("the subject: %w", err)
	}
	hasItem, hasElement, hasWidth := seen[subjectItem], seen[subjectElement], seen[subjectWidth]
	if !hasItem {
		if hasElement || hasWidth {
			return nil, errRootNamesPart
		}
		return Root{}, nil
	}
	of, err := idBits(partItem, item)
	if err != nil {
		return nil, fmt.Errorf("the subject's item: %w", err)
	}
	if hasWidth && (width < minWidth || width > fullWidth) {
		return nil, fmt.Errorf("the subject's segment width is %d, not %d to %d: %w", width, minWidth, fullWidth, errWidth)
	}
	w := int(width)
	if !hasElement {
		if hasWidth {
			return NewItem{ID: ItemID{b: of}, Width: w}, nil
		}
		return Item{ID: ItemID{b: of}}, nil
	}
	el, err := idBits(partElement, element)
	if err != nil {
		return nil, fmt.Errorf("the subject's element: %w", err)
	}
	if hasWidth {
		return NewElement{Of: ItemID{b: of}, ID: ElementID{b: el}, Width: w}, nil
	}
	return Element{Of: ItemID{b: of}, ID: ElementID{b: el}}, nil
}

// idBits reads an id's stored 16 bytes, the field what.
func idBits(what part, b []byte) ([16]byte, error) {
	var id [16]byte
	if err := sized(what, b, len(id)); err != nil {
		return id, err
	}
	copy(id[:], b)
	return id, nil
}

// readAttribution reads an attribution's stored form.
func readAttribution(b []byte) (Attribution, error) {
	a := Attribution{Agent: Unlabeled{}}
	r := wire.NewReader(b)
	var err error
	seen, err := readOnce(r, map[uint32]part{1: partCheckout, 2: partPrincipal, 3: partAgent}, func(tag uint32, r *wire.Reader) error {
		switch tag {
		case 1:
			c := r.Bytes()
			copy(a.Checkout[:], c)
			err = sized(partCheckout, c, len(a.Checkout))
		case 2:
			a.Principal, err = readPrincipal(r.Bytes())
		case 3:
			a.Agent = AgentFrom(string(r.Bytes()))
		}
		return err
	})
	switch {
	case err != nil:
		return Attribution{}, fmt.Errorf("the attribution: %w", err)
	case !seen[1]:
		return Attribution{}, fmt.Errorf("the attribution: %w", missing{What: partCheckout})
	case !seen[2]:
		return Attribution{}, fmt.Errorf("the attribution: %w", missing{What: partPrincipal})
	}
	return a, nil
}

// Wire is the principal as it is stored: its suite and its key.
// readPrincipal reads it.
func (p PrincipalID) wire() []byte {
	var w wire.Writer
	w.Uvarint(1, uint64(p.suite))
	w.Bytes(2, []byte(p.key))
	return w.Encoded()
}

// readPrincipal reads a principal's stored form.
func readPrincipal(b []byte) (PrincipalID, error) {
	var p PrincipalID
	r := wire.NewReader(b)
	seen, err := readOnce(r, map[uint32]part{1: partSuite, 2: partKey}, func(tag uint32, r *wire.Reader) error {
		switch tag {
		case 1:
			suite, err := byteOf(r)
			if err != nil {
				return err
			}
			p.suite = SuiteID(suite)
		case 2:
			p.key = string(r.Bytes())
		}
		return nil
	})
	switch {
	case err != nil:
		return PrincipalID{}, fmt.Errorf("the principal: %w", err)
	case !seen[1]:
		return PrincipalID{}, fmt.Errorf("the principal: %w", missing{What: partSuite})
	}
	return p, nil
}

// readObjectID reads what objectIDWire writes; what names the field in a
// refusal.
func readObjectID(b []byte, what string) (git.ObjectID, error) {
	var format git.ObjectFormat
	var sum []byte
	r := wire.NewReader(b)
	_, err := readOnce(r, map[uint32]part{1: partObjectFormat, 2: partObjectHash}, func(tag uint32, r *wire.Reader) error {
		switch tag {
		case 1:
			f, err := byteOf(r)
			if err != nil {
				return err
			}
			format = git.ObjectFormat(f)
		case 2:
			sum = r.Bytes()
		}
		return nil
	})
	if err != nil {
		return git.ObjectID{}, fmt.Errorf("%s: %w", what, err)
	}
	id, err := git.ObjectIDFrom(format, sum)
	if err != nil {
		return git.ObjectID{}, fmt.Errorf("%s: %w", what, err)
	}
	return id, nil
}
