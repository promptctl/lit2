package events

import "fmt"

// part is one field a decoder reads: the identity a refusal names, so a
// test names a refusal by a constant and never by its prose, and a
// field's one spelling is its constant's name `[LAW:one-source-of-truth]`.
// Which member, which role or how many bytes is the wrap site's to say,
// never the identity's.
type part uint8

// The parts run so every message's fields are in its tag order, a second
// listing of each message's fields the tests hold its declaration to: a
// declaration naming two fields by each other's part is out of this order.
const (
	_ part = iota

	// a record
	partHeader
	partSignature
	partBody

	// a signature, and a principal's or a sealed form's suite
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
	partProse
	partPlain

	// a placement and its forms
	partPlacement
	partLevel
	partParent

	// the payloads' fields, the policy payloads' among them
	partFrom
	partTo
	partField
	partValue
	partBase
	partMember
	partThreshold
	partVersion
	partBlob
	partOver
	partObserved
	partCarried
	partCommentBody
	partOwnerRole
	partRole
	partRoleName
	partCapability
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
	partProse: "prose form", partPlain: "plain form",
	partPlacement: "placement", partLevel: "level", partParent: "parent",
	partFrom: "from", partTo: "to", partField: "field", partValue: "value", partBase: "base policy",
	partMember: "member", partThreshold: "threshold", partVersion: "version", partBlob: "blob",
	partOver: "over", partObserved: "observed", partCarried: "carried field",
	partCommentBody: "comment's body", partOwnerRole: "owner role", partRole: "role", partRoleName: "role's name",
	partCapability: "capability", partMajority: "majority", partFixed: "fixed number", partProposal: "proposal",
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
