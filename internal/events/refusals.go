package events

import (
	"fmt"

	"github.com/promptctl/lit2/internal/events/wire"
)

// part is one field a decoder reads: the identity a refusal names, so a
// test names a refusal by a constant and never by its prose, and a
// field's one spelling is its constant's name `[LAW:one-source-of-truth]`.
// Which member, which role or how many bytes is the wrap site's to say,
// never the identity's.
type part uint8

// Each message's fields, in tag order.
const (
	_ part = iota

	// a record
	partHeader
	partSignature
	partBody

	// a signature, and a principal's or a sealed envelope's suite
	partSuite
	partSignatureBytes

	// a record's body
	partSalt
	partPayload

	// a header
	partKind
	partSchema
	partSubject
	partAttribution
	partConfigRef
	partPolicy
	partPosition
	partRecordedAt
	partPayloadHash

	// a subject
	partItem
	partElement
	partWidth

	// an attribution, and a principal's key
	partCheckout
	partPrincipal
	partAgent
	partKey

	// a git object id
	partObjectFormat
	partObjectHash

	// an event id, stored and in a founding file
	partEventID
	partEventIDText

	// an envelope and its forms
	partEnvelope
	partPlaintext
	partSealed
	partCommitment
	partText
	partTier
	partEpoch
	partNonce
	partCiphertext

	// a value and its forms
	partValue
	partProse
	partPlain

	// a placement and its forms
	partPlacement
	partLevel
	partParent

	// the payloads' fields
	partField
	partMember
	partFrom
	partTo
	partCommentBody
	partVersion
	partBlob

	// the policy payloads' fields
	partOwnerRole
	partRole
	partRoleName
	partBase
	partThreshold
	partMajority
	partFixed
	partProposal

	parts // the count, past the last part
)

// partNames is each part as a refusal says it.
var partNames = [parts]string{
	partHeader: "header", partSignature: "signature", partBody: "body",
	partSuite: "suite", partSignatureBytes: "signature bytes",
	partSalt: "salt", partPayload: "payload",
	partKind: "kind", partSchema: "schema version", partSubject: "subject", partAttribution: "attribution",
	partConfigRef: "config-ref", partPolicy: "policy version", partPosition: "causal position",
	partRecordedAt: "recorded-at", partPayloadHash: "payload hash",
	partItem: "item id", partElement: "element id", partWidth: "segment width",
	partCheckout: "checkout id", partPrincipal: "principal", partAgent: "agent", partKey: "key",
	partObjectFormat: "object format", partObjectHash: "object hash",
	partEventID: "event id", partEventIDText: "event id's text before its line ending",
	partEnvelope: "envelope", partPlaintext: "plaintext form", partSealed: "sealed form", partCommitment: "commitment",
	partText: "text", partTier: "tier", partEpoch: "epoch", partNonce: "nonce", partCiphertext: "ciphertext",
	partValue: "value", partProse: "prose form", partPlain: "plain form",
	partPlacement: "placement", partLevel: "level", partParent: "parent",
	partField: "field", partMember: "member", partFrom: "from", partTo: "to", partCommentBody: "comment's body",
	partVersion: "version", partBlob: "blob",
	partOwnerRole: "owner role", partRole: "role", partRoleName: "role's name", partBase: "base policy",
	partThreshold: "threshold", partMajority: "majority", partFixed: "fixed number", partProposal: "proposal",
}

func (p part) String() string { return partNames[p] }

// The decoders' shared refusals, each one type wherever it is met, with
// the field it is about as a value, so two refusals of one kind stay two
// refusals `[LAW:one-type-per-behavior]`: a field a message cannot be
// without is absent, a field a message gives once is given again, a
// fixed-length field is another length, a header field is at the zero no
// event carries, and a message of a sum holds other than one of its forms.
// Each is comparable, so errors.Is names one refusal by its value.
type (
	missing    struct{ What part }
	twice      struct{ What part }
	wrongSize  struct{ What part }
	zeroField  struct{ What part }
	notOneForm struct{ What part }
)

func (m missing) Error() string    { return "no " + m.What.String() }
func (t twice) Error() string      { return t.What.String() + " given twice" }
func (s wrongSize) Error() string  { return s.What.String() + " of the wrong length" }
func (z zeroField) Error() string  { return "a zero " + z.What.String() }
func (f notOneForm) Error() string { return f.What.String() + " not in one form" }

// sized is b's refusal as what, a field of want bytes, when b is another
// length: the one check of a fixed-length field, saying both lengths.
func sized(what part, b []byte, want int) error {
	if len(b) == want {
		return nil
	}
	return fmt.Errorf("%w: %d bytes, not %d", wrongSize{What: what}, len(b), want)
}

// readOnce reads r's fields with f, refusing a second of any field once
// names, and answers which tags it met. It is the one judge of a repeated
// field `[LAW:single-enforcer]`: protobuf keeps the last of a singular
// field, but a signed message read as its second value says what its
// writer never wrote, so every decoder refuses one alike. A tag once does
// not name, a list's element or a newer binary's field, is f's alone.
func readOnce(r *wire.Reader, once map[uint32]part, f func(uint32, *wire.Reader) error) (map[uint32]bool, error) {
	seen := map[uint32]bool{}
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		if p, ok := once[tag]; ok {
			if seen[tag] {
				return twice{What: p}
			}
			seen[tag] = true
		}
		return f(tag, r)
	})
	return seen, err
}
