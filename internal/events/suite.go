package events

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strings"
)

// SuiteID names a crypto suite. Every signature and principal carries one,
// so a second suite verifies beside the first (architecture.md §8).
type SuiteID uint8

// SuiteEd25519 is Ed25519 (RFC 8032) as Go's crypto/ed25519 implements it,
// the one suite at this slice (decision E).
const SuiteEd25519 SuiteID = 1

// Suite judges a signature of one suite.
type Suite interface {
	ID() SuiteID
	// Verify answers whether sig signs message under the public key.
	Verify(key, message, sig []byte) bool
}

// Ed25519 is the Ed25519 suite.
func Ed25519() Suite { return ed25519Suite{} }

type ed25519Suite struct{}

func (ed25519Suite) ID() SuiteID { return SuiteEd25519 }

// Verify is crypto/ed25519's strict verification, under which a valid
// signature has no second valid encoding, so an event id names one event.
func (ed25519Suite) Verify(key, message, sig []byte) bool {
	return len(key) == ed25519.PublicKeySize && ed25519.Verify(ed25519.PublicKey(key), message, sig)
}

// Suites is every suite this binary verifies, built once at the
// composition root and never changed.
type Suites struct{ by map[SuiteID]Suite }

// NewSuites is the suites ss; two of one id is a build fault.
func NewSuites(ss ...Suite) Suites {
	by := map[SuiteID]Suite{}
	for _, s := range ss {
		if _, ok := by[s.ID()]; ok {
			panic(fmt.Sprintf("events: two suites of id %d", s.ID()))
		}
		by[s.ID()] = s
	}
	return Suites{by: by}
}

// SignatureVerdict is what a signature check found.
type SignatureVerdict uint8

// The three verdicts of a signature check.
const (
	Valid SignatureVerdict = iota + 1
	Invalid
	UnknownSuite
)

func (v SignatureVerdict) String() string {
	switch v {
	case Valid:
		return "valid"
	case Invalid:
		return "invalid"
	case UnknownSuite:
		return "unknown_suite"
	}
	return fmt.Sprintf("no verdict (%d)", uint8(v))
}

// Check judges s as p's signature of message: a suite this binary does not
// have is UnknownSuite, never Invalid, since a newer binary may verify it.
func (ss Suites) Check(p PrincipalID, message []byte, s Signature) SignatureVerdict {
	suite, ok := ss.by[s.Suite]
	switch {
	case !ok:
		return UnknownSuite
	case s.Suite == p.suite && suite.Verify([]byte(p.key), message, s.Bytes):
		return Valid
	}
	return Invalid
}

// Signature is a signature and the suite that made it.
type Signature struct {
	Suite SuiteID
	Bytes []byte
}

// PrincipalID is a principal's public key and its suite: the only identity
// form that enters the store (§3.17). It is comparable, so it keys a map.
type PrincipalID struct {
	suite SuiteID
	key   string
}

// ed25519Codec is the multicodec of an Ed25519 public key, 0xed, as its
// unsigned varint: did:key's prefix for the key bytes.
var ed25519Codec = []byte{0xed, 0x01}

// didKey is the did:key method's prefix with multibase's base58btc mark.
const didKey = "did:key:z"

// String is the principal as did:key, did:key:z6Mk… for Ed25519. A
// principal of a suite this binary does not know, read from a newer
// writer's event, has no did:key here and prints its suite and key.
func (p PrincipalID) String() string {
	if p.suite != SuiteEd25519 {
		return fmt.Sprintf("suite-%d:%s", p.suite, base64.RawURLEncoding.EncodeToString([]byte(p.key)))
	}
	return didKey + base58(append(append([]byte{}, ed25519Codec...), p.key...))
}

// ParsePrincipalID reads an Ed25519 did:key.
func ParsePrincipalID(s string) (PrincipalID, error) {
	enc, ok := strings.CutPrefix(s, didKey)
	if !ok {
		return PrincipalID{}, errors.New("a principal is a did:key, did:key:z then base58btc")
	}
	b, err := unbase58(enc)
	if err != nil {
		return PrincipalID{}, fmt.Errorf("a principal's did:key: %w", err)
	}
	key, ok := strings.CutPrefix(string(b), string(ed25519Codec))
	if !ok || len(key) != ed25519.PublicKeySize {
		return PrincipalID{}, errors.New("a principal's did:key is not an Ed25519 public key: 0xed01 then 32 bytes")
	}
	return PrincipalID{suite: SuiteEd25519, key: key}, nil
}

// Signer signs as one principal; perform is its one caller (§5 step 6).
type Signer interface {
	Principal() PrincipalID
	Sign(message []byte) Signature
}

type ed25519Signer struct{ private ed25519.PrivateKey }

func (s ed25519Signer) Principal() PrincipalID {
	return PrincipalID{suite: SuiteEd25519, key: string(s.private.Public().(ed25519.PublicKey))}
}

func (s ed25519Signer) Sign(message []byte) Signature {
	return Signature{Suite: SuiteEd25519, Bytes: ed25519.Sign(s.private, message)}
}

// Format prints the principal under every verb, %#v and %x included, as
// Key does: the private key holds the seed.
func (s ed25519Signer) Format(f fmt.State, verb rune) {
	printPrincipal(f, verb, s.Principal(), "events.Signer")
}

// The base58btc alphabet, Bitcoin's.
const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// base58 is b in base58btc: each leading zero byte a 1, then the rest as a
// big-endian number.
func base58(b []byte) string {
	zeros := len(b) - len(strings.TrimLeft(string(b), "\x00"))
	n, radix, digit := new(big.Int).SetBytes(b), big.NewInt(58), new(big.Int)
	var out []byte
	for n.Sign() > 0 {
		n.DivMod(n, radix, digit)
		out = append(out, base58Alphabet[digit.Int64()])
	}
	out = append(out, strings.Repeat("1", zeros)...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

// unbase58 is base58's inverse.
func unbase58(s string) ([]byte, error) {
	n, radix := new(big.Int), big.NewInt(58)
	for i := range len(s) {
		d := strings.IndexByte(base58Alphabet, s[i])
		if d < 0 {
			return nil, fmt.Errorf("character %d is not base58btc", i+1)
		}
		n.Mul(n, radix).Add(n, big.NewInt(int64(d)))
	}
	zeros := len(s) - len(strings.TrimLeft(s, "1"))
	return append(make([]byte, zeros), n.Bytes()...), nil
}
