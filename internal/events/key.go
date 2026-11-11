package events

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	_ "embed" // the BIP-39 English word list
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Key is a principal's private key at rest: the Ed25519 seed, RFC 8032's
// private key, from which crypto/ed25519 derives the key pair. Printing a
// Key in any verb prints its principal, never the seed.
type Key struct {
	suite SuiteID
	seed  [ed25519.SeedSize]byte
}

// NewKey mints a key from 32 bytes of entropy.
func NewKey(entropy io.Reader) (Key, error) {
	k := Key{suite: SuiteEd25519}
	if _, err := io.ReadFull(entropy, k.seed[:]); err != nil {
		return Key{}, fmt.Errorf("drawing a principal's key: %w", err)
	}
	return k, nil
}

func (k Key) private() ed25519.PrivateKey { return ed25519.NewKeyFromSeed(k.seed[:]) }

// Signer signs as the key's principal.
func (k Key) Signer() Signer { return ed25519Signer{private: k.private()} }

// Principal is the key's public half, the principal id.
func (k Key) Principal() PrincipalID { return k.Signer().Principal() }

func (k Key) String() string   { return k.Principal().String() }
func (k Key) GoString() string { return "events.Key{" + k.Principal().String() + "}" }

// Format prints the principal under every verb, %#v and %x included.
func (k Key) Format(f fmt.State, verb rune) {
	if verb == 'v' && f.Flag('#') {
		_, _ = io.WriteString(f, k.GoString())
		return
	}
	_, _ = io.WriteString(f, k.String())
}

// keyFile is the key file's first word, its suite.
const keyFile = "ed25519 "

// FileText is the key in the key file's form: "ed25519 ", the seed in
// unpadded base64url, and a newline. ParseKeyFile reads it.
func (k Key) FileText() []byte {
	return []byte(keyFile + base64.RawURLEncoding.EncodeToString(k.seed[:]) + "\n")
}

// ParseKeyFile reads the key file's form, exactly. No error carries the
// file's bytes, since they are a secret.
func ParseKeyFile(b []byte) (Key, error) {
	if form := len(keyFile) + base64.RawURLEncoding.EncodedLen(ed25519.SeedSize) + 1; len(b) != form {
		return Key{}, fmt.Errorf("the key file holds %d bytes; its form is %d", len(b), form)
	}
	text, ok := bytes.CutPrefix(b, []byte(keyFile))
	if !ok {
		return Key{}, errors.New("the key file does not start with its suite, ed25519")
	}
	text, ok = bytes.CutSuffix(text, []byte("\n"))
	if !ok {
		return Key{}, errors.New("the key file does not end in a newline")
	}
	k := Key{suite: SuiteEd25519}
	if n, err := base64.RawURLEncoding.Strict().Decode(k.seed[:], text); err != nil || n != len(k.seed) {
		return Key{}, errors.New("the key file's seed is not 32 bytes in unpadded base64url")
	}
	return k, nil
}

// Phrase is the key's recovery phrase: the BIP-39 mnemonic whose 256-bit
// entropy is the Ed25519 seed. BIP-39's PBKDF2 "seed" is never derived; the
// phrase stands for the key itself.
type Phrase struct{ words [phraseWords]string }

// phraseWords is BIP-39's word count for 256 bits of entropy and their
// 8-bit checksum: 264 bits, 11 to a word.
const phraseWords = 24

//go:embed bip39-english.txt
var englishText string

// english is BIP-39's English word list, in order; a word's index is its
// 11-bit value. It is read once and never changed.
var english, englishIndex = wordList(englishText)

func wordList(text string) ([2048]string, map[string]uint16) {
	ws := strings.Fields(text)
	if len(ws) != 2048 {
		panic(fmt.Sprintf("events: the BIP-39 word list holds %d words", len(ws)))
	}
	var list [2048]string
	index := map[string]uint16{}
	for i, w := range ws {
		list[i], index[w] = w, uint16(i)
	}
	return list, index
}

// Phrase is the key's 24 words.
func (k Key) Phrase() Phrase {
	// 264 bits, the entropy then its 8-bit checksum, and two zero bytes
	// past the end, so each word reads three bytes
	var bits [35]byte
	copy(bits[:], k.seed[:])
	bits[32] = sha256.Sum256(k.seed[:])[0]
	var p Phrase
	for i := range p.words {
		at := 11 * i
		v := uint32(bits[at/8])<<16 | uint32(bits[at/8+1])<<8 | uint32(bits[at/8+2])
		p.words[i] = english[v>>(13-at%8)&0x7ff]
	}
	return p
}

// String is the words, separated by spaces.
func (p Phrase) String() string { return strings.Join(p.words[:], " ") }

// ParsePhrase reads a recovery phrase back into its key: 24 words of the
// English list, separated by white space, whose checksum holds. No error
// carries a word, since the phrase is a secret.
func ParsePhrase(s string) (Key, error) {
	ws := strings.Fields(s)
	if len(ws) != phraseWords {
		return Key{}, fmt.Errorf("a recovery phrase is %d words; this is %d", phraseWords, len(ws))
	}
	var bits [33]byte
	for i, w := range ws {
		v, ok := englishIndex[w]
		if !ok {
			return Key{}, fmt.Errorf("word %d of the recovery phrase is not in the BIP-39 English list", i+1)
		}
		for j := range 11 {
			if v>>(10-j)&1 == 1 {
				at := 11*i + j
				bits[at/8] |= 0x80 >> (at % 8)
			}
		}
	}
	k := Key{suite: SuiteEd25519}
	copy(k.seed[:], bits[:32])
	if sha256.Sum256(k.seed[:])[0] != bits[32] {
		return Key{}, errors.New("the recovery phrase's checksum does not hold: a word is wrong or out of place")
	}
	return k, nil
}
