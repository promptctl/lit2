package events

import (
	"crypto/ed25519"
	"testing"

	"filippo.io/edwards25519"

	"github.com/promptctl/lit2/internal/events/wire"
)

// A suite stored as 257 must not read as suite 1, which a byte of 257 wraps
// to. A suite of 1 is the control that reads.
func TestASuiteWiderThanAByteIsRefused(t *testing.T) {
	stored := func(suite uint64) []byte {
		var w wire.Writer
		w.Uvarint(1, suite)
		w.Bytes(2, []byte("k"))
		return w.Encoded()
	}
	if _, err := readPrincipal(stored(257)); err == nil {
		t.Error("a principal of suite 257 reads; want it refused")
	}
	if _, err := readSignature(stored(257)); err == nil {
		t.Error("a signature of suite 257 reads; want it refused")
	}
	if _, err := readPrincipal(stored(uint64(SuiteEd25519))); err != nil {
		t.Errorf("a principal of suite 1 does not read: %v", err)
	}
}

// The all-zero key is a point of small order. Under it, the signature whose R
// is the base point and whose S is 1 satisfies sB = R + k·0 for every message,
// so it would verify anything; so would a zero signature, whose R is of small
// order. Over many messages neither may verify, while a real key's signature
// still does.
func TestASmallOrderKeyVerifiesNothing(t *testing.T) {
	zeroKey := make([]byte, ed25519.PublicKeySize)
	one := make([]byte, ed25519.PublicKeySize)
	one[0] = 1
	forged := append(edwards25519.NewGeneratorPoint().Bytes(), one...)
	zeroSig := make([]byte, ed25519.SignatureSize)
	for i := range 100 {
		msg := []byte{byte(i)}
		if Ed25519().Verify(zeroKey, msg, zeroSig) || Ed25519().Verify(zeroKey, msg, forged) {
			t.Fatalf("the all-zero key verifies a forgery of %v; want it refused", msg)
		}
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("signed")
	if !Ed25519().Verify(pub, msg, ed25519.Sign(priv, msg)) {
		t.Error("a real key's signature does not verify; the control must")
	}
}
