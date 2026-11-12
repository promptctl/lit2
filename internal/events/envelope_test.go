package events

import (
	"bytes"
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
	both := append(SealPlain("a").wire(), lying.Encoded()...)
	for name, b := range map[string][]byte{
		"a plaintext whose commitment is not its text's": lying.Encoded(),
		"two forms":           both,
		"no commitment":       futureField(envelopePlain, string(p.Encoded())),
		"a short commitment":  futureField(envelopeCommitment, "short"),
		"bytes that are none": {0xff},
	} {
		if e, err := readEnvelope(b); err == nil {
			t.Errorf("%s reads as %#v; want it refused", name, e)
		}
	}
}
