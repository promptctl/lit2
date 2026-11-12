package events

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
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

// Wire is the id as it is stored: its 32 bytes. ReadEventID reads it.
func (id EventID) Wire() []byte { return slices.Clone(id[:]) }

// ReadEventID reads an id's stored form.
func ReadEventID(b []byte) (EventID, error) {
	if len(b) != len(EventID{}) {
		return EventID{}, fmt.Errorf("an event id is %d bytes, not %d", len(EventID{}), len(b))
	}
	return EventID(b), nil
}

// CausalPosition is an event's Lamport timestamp, 1 and up. Only Decode,
// Mint and ReadPosition make one, so a position is minted in Mint alone
// and never forged by a caller.
type CausalPosition struct{ n uint64 }

// Wire is the position as it is stored: its varint. ReadPosition reads it.
func (p CausalPosition) Wire() []byte { return binary.AppendUvarint(nil, p.n) }

// ReadPosition reads a position's stored form.
func ReadPosition(b []byte) (CausalPosition, error) {
	n, size := binary.Uvarint(b)
	if size <= 0 || size != len(b) || n == 0 {
		return CausalPosition{}, errors.New("a causal position is one varint, 1 or more")
	}
	return CausalPosition{n: n}, nil
}

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
	var hasHeader, hasSig bool
	r := wire.NewReader(b)
	for tag, ok := r.Next(); ok; tag, ok = r.Next() {
		switch tag {
		case recordHeader:
			rec.header, hasHeader = bytes.Clone(r.Bytes()), true
		case recordSignature:
			rec.sigRaw, hasSig = bytes.Clone(r.Bytes()), true
		case recordBody:
			rec.body, rec.hasBody = bytes.Clone(r.Bytes()), true
		}
	}
	switch err := r.Err(); {
	case err != nil:
		return record{}, fmt.Errorf("the record: %w", err)
	case !hasHeader:
		return record{}, errors.New("the record has no header")
	case !hasSig:
		return record{}, errors.New("the record has no signature")
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
	var hasSuite bool
	r := wire.NewReader(b)
	for tag, ok := r.Next(); ok; tag, ok = r.Next() {
		switch tag {
		case 1:
			s.Suite, hasSuite = SuiteID(r.Uvarint()), true
		case 2:
			s.Bytes = bytes.Clone(r.Bytes())
		}
	}
	switch err := r.Err(); {
	case err != nil:
		return Signature{}, fmt.Errorf("the signature: %w", err)
	case !hasSuite:
		return Signature{}, errors.New("the signature names no suite")
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
	var hasSalt, hasPayload bool
	r := wire.NewReader(rec.body)
	for tag, ok := r.Next(); ok; tag, ok = r.Next() {
		switch tag {
		case 1:
			b := r.Bytes()
			hasSalt = len(b) == len(salt)
			copy(salt[:], b)
		case 2:
			payload, hasPayload = bytes.Clone(r.Bytes()), true
		}
	}
	// a body that does not parse, or whose hash is not the header's, is
	// not the body the writer signed
	if r.Err() != nil || !hasSalt || !hasPayload || payloadHash(salt, payload) != h.payload {
		return Uncommitted{}
	}
	return k.read(h.kind, h.schema, salt, payload)
}

// wire is the header as Mint writes it, which readHeader reads.
func (h Header) wire() []byte {
	var w wire.Writer
	w.Bytes(headerKind, []byte(h.kind))
	w.Uvarint(headerSchema, uint64(h.schema))
	w.Bytes(headerSubject, AboutWire(h.about))
	w.Bytes(headerAttribution, h.by.Wire())
	w.Bytes(headerConfigRef, configRefWire(h.configRef))
	w.Uvarint(headerPolicy, uint64(h.policy))
	w.Uvarint(headerPosition, h.position.n)
	w.Uvarint(headerRecordedAt, uint64(h.recordedAt.UnixNano()))
	w.Bytes(headerPayloadHash, h.payload[:])
	return w.Encoded()
}

// AboutWire is a subject as it is stored. ReadAbout reads it.
func AboutWire(a About) []byte {
	var w wire.Writer
	switch a := a.(type) {
	case Root:
	case Item:
		w.Bytes(1, []byte(a.ID.s))
	case Element:
		w.Bytes(1, []byte(a.Of.s))
		w.Bytes(2, []byte(a.Name.s))
	default:
		// [LAW:no-silent-failure] an empty subject reads as the root, so a
		// draft naming nothing would mint a signed event about the root
		panic(fmt.Sprintf("events: %T is not an About", a))
	}
	return w.Encoded()
}

// Wire is the attribution as it is stored. ReadAttribution reads it.
func (a Attribution) Wire() []byte {
	var w wire.Writer
	w.Bytes(1, a.Checkout[:])
	w.Bytes(2, a.Principal.Wire())
	switch agent := a.Agent.(type) {
	case Labeled:
		w.Bytes(3, []byte(agent.label))
	case Unlabeled:
	default:
		// [LAW:no-silent-failure] no label reads back as Unlabeled, so a
		// Context naming no agent would sign a claim it never made
		panic(fmt.Sprintf("events: %T is not an Agent", a.Agent))
	}
	return w.Encoded()
}

// configRefWire is the id's format and bytes.
func configRefWire(id git.ObjectID) []byte {
	if id.Format() == 0 {
		// [LAW:types-are-the-program] exception: git.ObjectID's zero value
		// names no object, yet every event names its config-ref; a Context
		// holds a parsed id, so this is a caller's bug.
		panic("events: a config-ref that names no object")
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

// headerNames names each header field, in tag order.
var headerNames = [...]string{"kind", "schema version", "subject", "attribution", "config-ref", "policy version",
	"causal position", "recorded-at", "payload hash"}

// readHeader reads a header's stored bytes. A header missing a field the
// event cannot be without, or a root carrying an element name, is refused.
func readHeader(b []byte) (Header, error) {
	var h Header
	var seen [headerPayloadHash + 1]bool
	r := wire.NewReader(b)
	var err error
	for tag, ok := r.Next(); ok && err == nil; tag, ok = r.Next() {
		if tag <= headerPayloadHash {
			seen[tag] = true
		}
		switch tag {
		case headerKind:
			h.kind = Kind(r.Bytes())
		case headerSchema:
			h.schema = SchemaVersion(r.Uvarint())
		case headerSubject:
			h.about, err = ReadAbout(r.Bytes())
		case headerAttribution:
			h.by, err = ReadAttribution(r.Bytes())
		case headerConfigRef:
			h.configRef, err = readConfigRef(r.Bytes())
		case headerPolicy:
			h.policy = PolicyVersion(r.Uvarint())
		case headerPosition:
			h.position = CausalPosition{n: r.Uvarint()}
		case headerRecordedAt:
			ns := r.Uvarint()
			if ns == 0 || ns > math.MaxInt64 {
				err = fmt.Errorf("the recorded-at is %d ns, which no clock past 1970 reads as a time", ns)
			}
			h.recordedAt = time.Unix(0, int64(ns)).UTC()
		case headerPayloadHash:
			c := r.Bytes()
			copy(h.payload[:], c)
			if len(c) != len(h.payload) {
				err = errors.New("the payload hash is not 32 bytes")
			}
		}
	}
	switch {
	case err != nil:
		return Header{}, fmt.Errorf("the header: %w", err)
	case r.Err() != nil:
		return Header{}, fmt.Errorf("the header: %w", r.Err())
	}
	for i, name := range headerNames {
		if !seen[i+1] {
			return Header{}, fmt.Errorf("the header has no %s", name)
		}
	}
	switch {
	case h.kind == "":
		return Header{}, errors.New("the header's kind is empty")
	case h.schema == 0:
		return Header{}, errors.New("the header's schema version is 0")
	case h.policy == 0:
		return Header{}, errors.New("the header's policy version is 0")
	case h.position.n == 0:
		return Header{}, errors.New("the header's causal position is 0")
	}
	return h, nil
}

// ReadAbout reads a subject's stored form.
func ReadAbout(b []byte) (About, error) {
	var node, name []byte
	var hasNode, hasName bool
	r := wire.NewReader(b)
	for tag, ok := r.Next(); ok; tag, ok = r.Next() {
		switch tag {
		case 1:
			node, hasNode = r.Bytes(), true
		case 2:
			name, hasName = r.Bytes(), true
		}
	}
	if err := r.Err(); err != nil {
		return nil, fmt.Errorf("the subject: %w", err)
	}
	switch {
	case !hasNode && hasName:
		return nil, errors.New("the subject is the root, which has no elements, and names one")
	case !hasNode:
		return Root{}, nil
	}
	id, err := ParseItemID(string(node))
	if err != nil {
		return nil, fmt.Errorf("the subject: %w", err)
	}
	if !hasName {
		return Item{ID: id}, nil
	}
	s, err := ParseSegment(string(name))
	if err != nil {
		return nil, fmt.Errorf("the subject's element: %w", err)
	}
	return Element{Of: id, Name: s}, nil
}

// ReadAttribution reads an attribution's stored form.
func ReadAttribution(b []byte) (Attribution, error) {
	a := Attribution{Agent: Unlabeled{}}
	var hasCheckout, hasPrincipal bool
	r := wire.NewReader(b)
	var err error
	for tag, ok := r.Next(); ok && err == nil; tag, ok = r.Next() {
		switch tag {
		case 1:
			c := r.Bytes()
			hasCheckout = len(c) == len(a.Checkout)
			copy(a.Checkout[:], c)
		case 2:
			a.Principal, err = ReadPrincipalID(r.Bytes())
			hasPrincipal = true
		case 3:
			a.Agent = AgentFrom(string(r.Bytes()))
		}
	}
	switch {
	case err != nil:
		return Attribution{}, err
	case r.Err() != nil:
		return Attribution{}, fmt.Errorf("the attribution: %w", r.Err())
	case !hasCheckout:
		return Attribution{}, errors.New("the attribution has no 16-byte checkout id")
	case !hasPrincipal:
		return Attribution{}, errors.New("the attribution has no principal")
	}
	return a, nil
}

// Wire is the principal as it is stored: its suite and its key.
// ReadPrincipalID reads it.
func (p PrincipalID) Wire() []byte {
	var w wire.Writer
	w.Uvarint(1, uint64(p.suite))
	w.Bytes(2, []byte(p.key))
	return w.Encoded()
}

// ReadPrincipalID reads a principal's stored form.
func ReadPrincipalID(b []byte) (PrincipalID, error) {
	var p PrincipalID
	var hasSuite bool
	r := wire.NewReader(b)
	for tag, ok := r.Next(); ok; tag, ok = r.Next() {
		switch tag {
		case 1:
			p.suite, hasSuite = SuiteID(r.Uvarint()), true
		case 2:
			p.key = string(r.Bytes())
		}
	}
	switch err := r.Err(); {
	case err != nil:
		return PrincipalID{}, fmt.Errorf("the principal: %w", err)
	case !hasSuite:
		return PrincipalID{}, errors.New("the principal names no suite")
	}
	return p, nil
}

func readConfigRef(b []byte) (git.ObjectID, error) {
	var format git.ObjectFormat
	var sum []byte
	r := wire.NewReader(b)
	for tag, ok := r.Next(); ok; tag, ok = r.Next() {
		switch tag {
		case 1:
			format = git.ObjectFormat(r.Uvarint())
		case 2:
			sum = r.Bytes()
		}
	}
	if err := r.Err(); err != nil {
		return git.ObjectID{}, fmt.Errorf("the config-ref: %w", err)
	}
	id, err := git.ObjectIDFrom(format, sum)
	if err != nil {
		return git.ObjectID{}, fmt.Errorf("the config-ref: %w", err)
	}
	return id, nil
}
