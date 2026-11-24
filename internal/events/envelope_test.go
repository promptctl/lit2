package events

import (
	"bytes"
	"errors"
	"testing"

	"github.com/promptctl/lit2/internal/events/wire"
)

func envelopeOf(t *testing.T, b []byte) Envelope {
	t.Helper()
	e, err := readEnvelope(b)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestAnEnvelopeOpensByItsForm(t *testing.T) {
	commitment := bytes.Repeat([]byte{7}, 32)
	var sealedForm, w, future wire.Writer
	sealedForm.Uvarint(1, uint64(SuiteEd25519))
	sealedForm.Bytes(2, []byte("team"))
	sealedForm.Uvarint(3, 4)
	sealedForm.Bytes(4, []byte("nonce"))
	sealedForm.Bytes(5, []byte("ciphertext"))
	w.Bytes(envelopeSealed, sealedForm.Encoded())
	w.Bytes(envelopeCommitment, commitment)
	future.Bytes(envelopeCommitment, commitment) // known fields first, as every writer puts them
	future.Bytes(7, []byte("a form from a newer binary"))

	for name, tc := range map[string]struct {
		e    Envelope
		want Opened
	}{
		"plain":       {SealPlain("the text"), Text{Text: "the text"}},
		"sealed":      {envelopeOf(t, w.Encoded()), Locked{Tier: "team"}},
		"a new form":  {envelopeOf(t, future.Encoded()), Unopenable{}},
		"plain again": {envelopeOf(t, SealPlain("read back").wire()), Text{Text: "read back"}},
	} {
		if got := Open(tc.e, NewKeyring()); got != tc.want {
			t.Errorf("%s opens %#v; want %#v", name, got, tc.want)
		}
	}
	if kept := envelopeOf(t, future.Encoded()); !bytes.Equal(kept.wire(), future.Encoded()) {
		t.Error("a form this binary does not know is not written back whole")
	}
	if sealedAgain := envelopeOf(t, w.Encoded()); !bytes.Equal(sealedAgain.wire(), w.Encoded()) {
		t.Error("a sealed envelope is not written back whole")
	}
}

func TestEqualTextsCommitEqual(t *testing.T) {
	if SealPlain("one").Commitment() != SealPlain(string([]byte{'o', 'n', 'e'})).Commitment() {
		t.Error("one text commits two ways")
	}
	if SealPlain("one").Commitment() == SealPlain("two").Commitment() {
		t.Error("two texts commit alike")
	}
	if envelopeOf(t, SealPlain("one").wire()).Commitment() != SealPlain("one").Commitment() {
		t.Error("an envelope read back commits differently")
	}
}

func TestAnEnvelopeThatLiesOrIsAmbiguousIsRefused(t *testing.T) {
	var lying wire.Writer
	var p wire.Writer
	p.Bytes(1, []byte("the text"))
	lying.Bytes(envelopePlain, p.Encoded())
	lying.Bytes(envelopeCommitment, bytes.Repeat([]byte{1}, 32))
	var both wire.Writer
	both.Bytes(envelopePlain, p.Encoded())
	both.Bytes(envelopeSealed, futureField(2, "tier"))
	commitment := SealPlain("the text").Commitment()
	both.Bytes(envelopeCommitment, commitment[:])
	// the control: a plaintext committed to its own text reads
	envelopeOf(t, SealPlain("the text").wire())
	none := []byte{0xff}
	for name, c := range map[string]struct {
		b    []byte
		want error
	}{
		"a plaintext whose commitment is not its text's": {lying.Encoded(), errCommitmentNotText},
		"two forms":           {both.Encoded(), notOneForm{What: partEnvelope}},
		"no commitment":       {futureField(envelopePlain, string(p.Encoded())), missing{What: partCommitment}},
		"a short commitment":  {futureField(envelopeCommitment, "short"), wrongSize{What: partCommitment}},
		"bytes that are none": {none, wireRefusal(t, none)},
	} {
		if e, err := readEnvelope(c.b); !errors.Is(err, c.want) {
			t.Errorf("%s reads as %#v, %v; want %v", name, e, err, c.want)
		}
	}
}
