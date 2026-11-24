package events

import (
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// repeated is b with a copy of its first field tagged tag appended: the
// field given twice, cut from b by protobuf's own key and length rule, so
// the decoder under test is not the judge of where the field lies.
func repeated(t *testing.T, b []byte, tag uint32) []byte {
	t.Helper()
	for at := 0; at < len(b); {
		start := at
		key, n := binary.Uvarint(b[at:])
		if n <= 0 {
			t.Fatalf("%x: a key at %d is no varint", b, at)
		}
		at += n
		switch key & 7 {
		case 0:
			_, n = binary.Uvarint(b[at:])
			if n <= 0 {
				t.Fatalf("%x: a varint at %d is torn", b, at)
			}
			at += n
		case 2:
			size, n := binary.Uvarint(b[at:])
			if n <= 0 || size > uint64(len(b)-at-n) {
				t.Fatalf("%x: a length at %d runs past the message", b, at)
			}
			at += n + int(size)
		default:
			t.Fatalf("%x: wire type %d at %d", b, key&7, start)
		}
		if uint32(key>>3) == tag {
			return append(append([]byte(nil), b...), b[start:at]...)
		}
	}
	t.Fatalf("%x holds no field %d", b, tag)
	return nil
}

func message(fields func(*wire.Writer)) []byte {
	var w wire.Writer
	fields(&w)
	return w.Encoded()
}

// payloadRead is a payload decoder over its bytes.
func payloadRead(read func(*wire.Reader) (Versioned, error)) func([]byte) error {
	return func(b []byte) error {
		_, err := read(wire.NewReader(b))
		return err
	}
}

// Every decoder refuses a field its message gives once when it is given
// again, as that field given twice, and reads the message without the
// repeat, the control, which differs from it in the repeat alone. Each
// list, a proposal's members and an enrollment's roles, refuses one key
// twice alike, and says which.
func TestEveryDecoderRefusesAFieldGivenTwice(t *testing.T) {
	k := keyOf(1)
	e := mint(t, context(1, Frontier{}, Known{}), k, 1, Act{About: Root{}, Payload: note{Text: text("x")}}).Events[0]
	a := k.Principal()
	owner, contributor := MustName("owner"), MustName("contributor")
	sealedForm := message(func(w *wire.Writer) {
		w.Uvarint(1, 1)
		w.Bytes(2, []byte("tier"))
		w.Uvarint(3, 1)
		w.Bytes(4, []byte("nonce"))
		w.Bytes(5, []byte("ciphertext"))
	})
	sealedEnvelope := message(func(w *wire.Writer) {
		w.Bytes(envelopeSealed, sealedForm)
		w.Bytes(envelopeCommitment, make([]byte, 32))
	})
	membership := message(func(w *wire.Writer) {
		w.Bytes(1, a.wire())
		w.Bytes(2, []byte("owner"))
	})
	field := TransitionField{Field: MustName("reason"), Value: Plain{Canonical: "x"}}.wire()
	record, signature, header := []byte(recordOf(e.raw, e.Signature(), bodyWire(t, e))), signatureWire(e.Signature()), e.raw
	subject := aboutWire(NewElement{Of: anItem, ID: anElement, Width: 4})
	attribution := Attribution{Checkout: store.CheckoutID{1}, Principal: a, Agent: AgentFrom("x")}.wire()
	objectID := objectIDWire(git.HashBlob(git.SHA1, aVersion), "blob")
	readObject := func(b []byte) error { _, err := readObjectID(b, "the blob"); return err }

	type decoder struct {
		name string
		read func([]byte) error
		b    []byte
	}
	decoders := []decoder{
		{"a record", func(b []byte) error { _, err := readRecord(b); return err }, record},
		{"a signature", func(b []byte) error { _, err := readSignature(b); return err }, signature},
		{"a header", func(b []byte) error { _, err := readHeader(b); return err }, header},
		{"a subject", func(b []byte) error { _, err := readAbout(b); return err }, subject},
		{"an attribution", func(b []byte) error { _, err := readAttribution(b); return err }, attribution},
		{"a principal", func(b []byte) error { _, err := readPrincipal(b); return err }, a.wire()},
		{"an object id", readObject, objectID},
		{"a plaintext envelope", func(b []byte) error { _, err := readEnvelope(b); return err }, SealPlain("x").wire()},
		{"a sealed envelope", func(b []byte) error { _, err := readEnvelope(b); return err }, sealedEnvelope},
		{"a plaintext form", func(b []byte) error { _, err := readPlain(b); return err }, futureField(1, "x")},
		{"a sealed form", func(b []byte) error { _, err := readSealed(b); return err }, sealedForm},
		{"a plain value", func(b []byte) error { _, err := readValue(b); return err }, Plain{Canonical: "b"}.wire()},
		{"a prose value", func(b []byte) error { _, err := readValue(b); return err }, Prose{Body: text("a")}.wire()},
		{"a placement at a level", func(b []byte) error { _, err := ReadPlacement(b); return err }, PlacementWire(AtLevel{Level: mustLevel(1)})},
		{"a placement below a parent", func(b []byte) error { _, err := ReadPlacement(b); return err }, PlacementWire(BelowParent{Parent: anItem})},
		{"an item.create", payloadRead(ReadItemCreate), payloadBytes(ItemCreate{Place: BelowParent{Parent: anItem}})},
		{"a field.set", payloadRead(ReadFieldSet), payloadBytes(FieldSet{Field: MustName("title"), Value: Plain{Canonical: "x"}})},
		{"a member.add", payloadRead(ReadMemberAdd), payloadBytes(MemberAdd{Field: labels, Member: MustName("a")})},
		{"a member.rm", payloadRead(ReadMemberRm), payloadBytes(MemberRm{Field: labels, Member: MustName("a")})},
		{"a transition", payloadRead(ReadTransition), payloadBytes(Transition{From: MustName("plan"), To: MustName("done")})},
		{"a transition's field", func(b []byte) error { _, err := readTransitionField(b); return err }, field},
		{"a comment.add", payloadRead(ReadCommentAdd), payloadBytes(CommentAdd{Body: text("c")})},
		{"a workspace.apply", payloadRead(ReadWorkspaceApply), payloadBytes(NewWorkspaceApply(aVersion, git.SHA1, nil))},
		{"an enrollment", payloadRead(ReadEnroll), payloadBytes(Enroll{Owner: owner, Roles: map[Name][]Capability{contributor: nil}})},
		{"a role", func(b []byte) error { _, _, err := readRole(b); return err }, futureField(1, "contributor")},
		{"a proposal", payloadRead(ReadPropose), payloadBytes(Propose{Base: Digest{3}, Members: map[PrincipalID]Name{a: owner}, Threshold: Majority{}})},
		{"a membership", func(b []byte) error { _, _, err := readMembership(b); return err }, membership},
		{"a majority threshold", func(b []byte) error { _, err := readThreshold(b); return err }, message(func(w *wire.Writer) { w.Bytes(thresholdMajority, nil) })},
		{"a fixed threshold", func(b []byte) error { _, err := readThreshold(b); return err }, message(func(w *wire.Writer) { w.Uvarint(thresholdFixed, 1) })},
		{"an approval", payloadRead(ReadApprove), payloadBytes(Approve{Proposal: Digest{1}})},
	}
	byName := map[string]decoder{}
	for _, d := range decoders {
		if err := d.read(d.b); err != nil {
			t.Fatalf("%s whole: %v; the control must read", d.name, err)
		}
		byName[d.name] = d
	}
	for _, c := range []struct {
		decoder string
		tag     uint32
		want    part
		says    string // what the refusal names beside the field, if anything
	}{
		{"a record", recordHeader, partHeader, ""},
		{"a record", recordSignature, partSignature, ""},
		{"a record", recordBody, partBody, ""},
		{"a signature", 1, partSuite, ""},
		{"a signature", 2, partSignatureBytes, ""},
		{"a header", headerKind, partKind, ""},
		{"a header", headerSchema, partSchema, ""},
		{"a header", headerSubject, partSubject, ""},
		{"a header", headerAttribution, partAttribution, ""},
		{"a header", headerConfigRef, partConfigRef, ""},
		{"a header", headerPolicy, partPolicy, ""},
		{"a header", headerPosition, partPosition, ""},
		{"a header", headerRecordedAt, partRecordedAt, ""},
		{"a header", headerPayloadHash, partPayloadHash, ""},
		{"a subject", subjectItem, partItem, ""},
		{"a subject", subjectElement, partElement, ""},
		{"a subject", subjectWidth, partWidth, ""},
		{"an attribution", 1, partCheckout, ""},
		{"an attribution", 2, partPrincipal, ""},
		{"an attribution", 3, partAgent, ""},
		{"a principal", 1, partSuite, ""},
		{"a principal", 2, partKey, ""},
		{"an object id", 1, partObjectFormat, ""},
		{"an object id", 2, partObjectHash, ""},
		{"a plaintext envelope", envelopePlain, partPlaintext, ""},
		{"a plaintext envelope", envelopeCommitment, partCommitment, ""},
		{"a sealed envelope", envelopeSealed, partSealed, ""},
		{"a plaintext form", 1, partText, ""},
		{"a sealed form", 1, partSuite, ""},
		{"a sealed form", 2, partTier, ""},
		{"a sealed form", 3, partEpoch, ""},
		{"a sealed form", 4, partNonce, ""},
		{"a sealed form", 5, partCiphertext, ""},
		{"a plain value", valuePlain, partPlain, ""},
		{"a prose value", valueProse, partProse, ""},
		{"a placement at a level", placeAtLevel, partLevel, ""},
		{"a placement below a parent", placeBelowParent, partParent, ""},
		{"an item.create", 1, partPlacement, ""},
		{"a field.set", 1, partField, ""},
		{"a field.set", 2, partValue, ""},
		{"a member.add", 1, partField, ""},
		{"a member.add", 2, partMember, ""},
		{"a member.rm", 1, partField, ""},
		{"a member.rm", 2, partMember, ""},
		{"a transition", 1, partFrom, ""},
		{"a transition", 2, partTo, ""},
		{"a transition's field", 1, partField, ""},
		{"a transition's field", 2, partValue, ""},
		{"a comment.add", 1, partCommentBody, ""},
		{"a workspace.apply", 1, partVersion, ""},
		{"a workspace.apply", 2, partBlob, ""},
		{"an enrollment", 1, partOwnerRole, ""},
		{"an enrollment", 2, partRole, "contributor"},
		{"a role", 1, partRoleName, ""},
		{"a proposal", 1, partBase, ""},
		{"a proposal", 2, partMember, a.String()},
		{"a proposal", 3, partThreshold, ""},
		{"a membership", 1, partPrincipal, ""},
		{"a membership", 2, partRole, ""},
		{"a majority threshold", thresholdMajority, partMajority, ""},
		{"a fixed threshold", thresholdFixed, partFixed, ""},
		{"an approval", 1, partProposal, ""},
	} {
		d, ok := byName[c.decoder]
		if !ok {
			t.Fatalf("no decoder %q", c.decoder)
		}
		err := d.read(repeated(t, d.b, c.tag))
		if !errors.Is(err, twice{What: c.want}) {
			t.Errorf("%s giving field %d twice reads with %v; want its %s given twice", c.decoder, c.tag, err, c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s giving field %d twice is refused as %q; want it to name %s", c.decoder, c.tag, err, c.says)
		}
	}
}

// A body giving its payload twice is not the body its writer signed, even
// when the second is the one the header's hash commits to; the body once
// is the control, which reads.
func TestABodyGivingAFieldTwiceIsUncommitted(t *testing.T) {
	k := keyOf(1)
	e := mint(t, context(1, Frontier{}, Known{}), k, 1, Act{About: Root{}, Payload: note{Text: text("x")}}).Events[0]
	body := bodyWire(t, e)
	if got, ok := decoded(t, here, recordOf(e.raw, e.Signature(), body)).Body().(Readable); !ok {
		t.Fatalf("the body once reads as %#v; the control must read", got)
	}
	for _, tag := range []uint32{1, 2} {
		if got := decoded(t, here, recordOf(e.raw, e.Signature(), repeated(t, body, tag))).Body(); got != (Uncommitted{}) {
			t.Errorf("a body giving field %d twice reads as %#v; want it uncommitted", tag, got)
		}
	}
}
