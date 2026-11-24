package events

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/promptctl/lit2/internal/events/wire"
)

// Envelope is one prose value, opaque below the verbs (INV:opaque-envelope):
// a layer below them compares two envelopes by Commitment and never reads
// one. Open is the only reader, and the architecture test refuses it
// outside root, verbs and cli `[LAW:single-enforcer]`.
type Envelope struct {
	form       form
	commitment Commitment
	unknown    wire.Unknown
	// [LAW:types-are-the-program] its forms hold bytes, so == would panic
	// at run time; this makes it a compile error, and Commitment the way
	_ [0]func()
}

// Commitment is a hash of an envelope's content, equal for equal texts.
type Commitment [32]byte

// form is how an envelope holds its content.
//
//sumtype:decl
type form interface{ form() }

// plain is the plaintext tier's content, the documented public mode.
type plain struct {
	text    []byte
	unknown wire.Unknown
}

// sealed is content encrypted under a tier's epoch key.
type sealed struct {
	suite             SuiteID
	tier              Tier
	epoch             Epoch
	nonce, ciphertext []byte
	unknown           wire.Unknown
}

// unknownForm is a form a newer binary wrote; its fields are kept in the
// envelope's unknown fields and written back whole.
type unknownForm struct{}

func (plain) form()       {}
func (sealed) form()      {}
func (unknownForm) form() {}

// Tier names a visibility group (§3.19).
type Tier string

// Epoch is a tier key's generation; removing a member starts a new one.
type Epoch uint64

// SealPlain is text in the plaintext tier.
func SealPlain(text string) Envelope {
	return Envelope{form: plain{text: []byte(text)}, commitment: plainCommitment([]byte(text))}
}

func plainCommitment(text []byte) Commitment {
	return sha256.Sum256(append([]byte("lit2 plain v1\x00"), text...))
}

// Commitment is the content's commitment: two envelopes holding one text
// commit equal, so the fold can tell two writes agree without opening
// either.
func (e Envelope) Commitment() Commitment { return e.commitment }

// Keyring is the tier keys this principal holds; at this slice it holds
// none, so a sealed envelope opens Locked.
type Keyring struct{}

// NewKeyring is the keyring of a principal holding no tier key.
func NewKeyring() Keyring { return Keyring{} }

// Opened is what opening an envelope gave.
//
//sumtype:decl
type Opened interface{ opened() }

// Text is an envelope's content, read.
type Text struct{ Text string }

// Locked is content sealed under a tier whose key the keyring lacks.
type Locked struct{ Tier Tier }

// Unopenable is a form this binary does not know.
type Unopenable struct{}

func (Text) opened()       {}
func (Locked) opened()     {}
func (Unopenable) opened() {}

// The envelope's refusals: a form no Envelope a caller can build holds, which
// only the zero Envelope has, and a plaintext committed to another text.
var (
	errNoForm            = errors.New("a type that is no envelope form")
	errCommitmentNotText = errors.New("a plaintext envelope whose commitment is not its text's")
)

// Open reads e with k's keys.
func Open(e Envelope, k Keyring) Opened {
	switch f := e.form.(type) {
	case plain:
		return Text{Text: string(f.text)}
	case sealed:
		return Locked{Tier: f.tier}
	case unknownForm:
		return Unopenable{}
	}
	panic(fmt.Errorf("events: %T: %w", e.form, errNoForm))
}

// The envelope's wire fields: envelope = 1:plain{1:text} | 2:sealed{1:suite
// 2:tier 3:epoch 4:nonce 5:ciphertext} · 3:commitment · unknown….
const (
	envelopePlain      = 1
	envelopeSealed     = 2
	envelopeCommitment = 3
)

// Wire is the envelope as a wire message, which readEnvelope reads.
func (e Envelope) wire() []byte {
	var w wire.Writer
	switch f := e.form.(type) {
	case plain:
		var p wire.Writer
		p.Bytes(1, f.text)
		p.Keep(f.unknown)
		w.Bytes(envelopePlain, p.Encoded())
	case sealed:
		var s wire.Writer
		s.Uvarint(1, uint64(f.suite))
		s.Bytes(2, []byte(f.tier))
		s.Uvarint(3, uint64(f.epoch))
		s.Bytes(4, f.nonce)
		s.Bytes(5, f.ciphertext)
		s.Keep(f.unknown)
		w.Bytes(envelopeSealed, s.Encoded())
	case unknownForm:
	default:
		// [LAW:no-silent-failure] the zero Envelope would read back as a
		// newer binary's form, hiding a payload field never set
		panic(fmt.Errorf("events: %T: %w", e.form, errNoForm))
	}
	w.Bytes(envelopeCommitment, e.commitment[:])
	w.Keep(e.unknown)
	return w.Encoded()
}

// envelopeFields is an envelope's fields: its form, nil when it is none
// this binary knows, and its commitment.
type envelopeFields struct {
	form       form
	commitment Commitment
}

// readEnvelope reads an envelope's wire message: one of its forms, and its
// commitment, which is no form. A form is the one thing an envelope's
// layer may change without its readers, since below the verbs an envelope
// is compared by commitment and never read (INV:opaque-envelope), so its
// sum is optional: an envelope holding neither form this binary knows and
// a field it does not know is a newer binary's form, kept whole and opened
// Unopenable (INV:preserve-unknown). One holding only its commitment holds
// no form at all, and is refused, as one holding both forms is, and a
// plaintext whose commitment is not its text's.
var readEnvelope = declare("an envelope", []field[envelopeFields]{
	{envelopePlain, partPlaintext, optional, func(e *envelopeFields, r *wire.Reader) (err error) {
		e.form, err = readPlain(r.Bytes())
		return err
	}},
	{envelopeSealed, partSealed, optional, func(e *envelopeFields, r *wire.Reader) (err error) {
		e.form, err = readSealed(r.Bytes())
		return err
	}},
	fixedField(envelopeCommitment, partCommitment, one, func(e *envelopeFields) []byte { return e.commitment[:] }),
}, func(e *envelopeFields, u wire.Unknown) (Envelope, error) {
	if p, ok := e.form.(plain); ok && plainCommitment(p.text) != e.commitment {
		return Envelope{}, errCommitmentNotText
	}
	if e.form == nil {
		// the sum's declaration admitted no form this binary knows only
		// beside a field it does not know: a newer binary's form
		e.form = unknownForm{}
	}
	return Envelope{form: e.form, commitment: e.commitment, unknown: u}, nil
}, forms{partEnvelope, optional, []uint32{envelopePlain, envelopeSealed}}).read

// readPlain reads a plaintext form: its text.
var readPlain = declare("a plaintext form", []field[plain]{
	{1, partText, one, func(p *plain, r *wire.Reader) error {
		p.text = bytes.Clone(r.Bytes())
		return nil
	}},
}, func(p *plain, u wire.Unknown) (plain, error) {
	p.unknown = u
	return *p, nil
}).read

// readSealed reads a sealed form: its suite, its tier and the tier key's
// epoch, its nonce and its ciphertext.
var readSealed = declare("a sealed form", []field[sealed]{
	{1, partSuite, one, func(s *sealed, r *wire.Reader) error {
		suite, err := byteOf(r)
		s.suite = SuiteID(suite)
		return err
	}},
	{2, partTier, one, func(s *sealed, r *wire.Reader) error {
		s.tier = Tier(r.Bytes())
		return nil
	}},
	{3, partEpoch, one, func(s *sealed, r *wire.Reader) error {
		s.epoch = Epoch(r.Uvarint())
		return nil
	}},
	{4, partNonce, one, func(s *sealed, r *wire.Reader) error {
		s.nonce = bytes.Clone(r.Bytes())
		return nil
	}},
	{5, partCiphertext, one, func(s *sealed, r *wire.Reader) error {
		s.ciphertext = bytes.Clone(r.Bytes())
		return nil
	}},
}, func(s *sealed, u wire.Unknown) (sealed, error) {
	s.unknown = u
	return *s, nil
}).read
