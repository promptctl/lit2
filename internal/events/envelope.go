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
	panic(fmt.Sprintf("events: %T is not an envelope's form", e.form))
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
		panic(fmt.Sprintf("events: %T is not an envelope's form", e.form))
	}
	w.Bytes(envelopeCommitment, e.commitment[:])
	w.Keep(e.unknown)
	return w.Encoded()
}

// readEnvelope reads an envelope's wire message. One holding neither form
// this binary knows is an unknownForm, its fields kept; one holding both,
// or a plaintext whose commitment is not its text's, is refused.
func readEnvelope(b []byte) (Envelope, error) {
	var e Envelope
	forms, committed := 0, false
	r := wire.NewReader(b)
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		switch tag {
		case envelopePlain:
			p, err := readPlain(r.Bytes())
			e.form, forms = p, forms+1
			return err
		case envelopeSealed:
			s, err := readSealed(r.Bytes())
			e.form, forms = s, forms+1
			return err
		case envelopeCommitment:
			c := r.Bytes()
			if len(c) != len(e.commitment) {
				return fmt.Errorf("an envelope's commitment is %d bytes, not %d", len(c), len(e.commitment))
			}
			copy(e.commitment[:], c)
			committed = true
		}
		return nil
	})
	if err != nil {
		return Envelope{}, fmt.Errorf("an envelope: %w", err)
	}
	e.unknown = r.Rest()
	switch {
	case forms > 1:
		return Envelope{}, errors.New("an envelope holds two forms")
	case !committed:
		return Envelope{}, errors.New("an envelope has no commitment")
	case forms == 0:
		e.form = unknownForm{}
	}
	if p, ok := e.form.(plain); ok && plainCommitment(p.text) != e.commitment {
		return Envelope{}, errors.New("a plaintext envelope's commitment is not its text's")
	}
	return e, nil
}

func readPlain(b []byte) (plain, error) {
	var p plain
	r := wire.NewReader(b)
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		if tag == 1 {
			p.text = bytes.Clone(r.Bytes())
		}
		return nil
	})
	p.unknown = r.Rest()
	if err != nil {
		return plain{}, fmt.Errorf("a plaintext envelope: %w", err)
	}
	return p, nil
}

func readSealed(b []byte) (sealed, error) {
	var s sealed
	r := wire.NewReader(b)
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		switch tag {
		case 1:
			s.suite = SuiteID(r.Uvarint())
		case 2:
			s.tier = Tier(r.Bytes())
		case 3:
			s.epoch = Epoch(r.Uvarint())
		case 4:
			s.nonce = bytes.Clone(r.Bytes())
		case 5:
			s.ciphertext = bytes.Clone(r.Bytes())
		}
		return nil
	})
	s.unknown = r.Rest()
	if err != nil {
		return sealed{}, fmt.Errorf("a sealed envelope: %w", err)
	}
	return s, nil
}
