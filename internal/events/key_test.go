package events

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// keyFrom is the key whose Ed25519 seed is the hex seed.
func keyFrom(t *testing.T, seed string) Key {
	t.Helper()
	b, err := hex.DecodeString(seed)
	if err != nil || len(b) != 32 {
		t.Fatalf("%s is not a 32-byte seed", seed)
	}
	k := Key{suite: SuiteEd25519}
	copy(k.seed[:], b)
	return k
}

func TestTheWordListIsBIP39sEnglishList(t *testing.T) {
	// the SHA-256 of bip-0039/english.txt as BIP-39 publishes it
	const published = "2f5eed53a4727b4bf8880d8f3f199efc90e58503646d9ff8eff3a2ed3b24dbda"
	if got := sha256.Sum256([]byte(englishText)); hex.EncodeToString(got[:]) != published {
		t.Errorf("the embedded word list hashes to %x; want BIP-39's %s", got, published)
	}
}

// bip39 is the 256-bit vectors of BIP-39's reference test set
// (trezor/python-mnemonic vectors.json, "english"): entropy, mnemonic.
var bip39 = [][2]string{
	{"0000000000000000000000000000000000000000000000000000000000000000",
		"abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"},
	{"7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f7f",
		"legal winner thank year wave sausage worth useful legal winner thank year wave sausage worth useful legal winner thank year wave sausage worth title"},
	{"8080808080808080808080808080808080808080808080808080808080808080",
		"letter advice cage absurd amount doctor acoustic avoid letter advice cage absurd amount doctor acoustic avoid letter advice cage absurd amount doctor acoustic bless"},
	{"ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		"zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo vote"},
	{"68a79eaca2324873eacc50cb9c6eca8cc68ea5d936f98787c60c7ebc74e6ce7c",
		"hamster diagram private dutch cause delay private meat slide toddler razor book happy fancy gospel tennis maple dilemma loan word shrug inflict delay length"},
	{"9f6a2878b2520799a44ef18bc7df394e7061a224d2c33cd015b157d746869863",
		"panda eyebrow bullet gorilla call smoke muffin taste mesh discover soft ostrich alcohol speed nation flash devote level hobby quick inner drive ghost inside"},
	{"066dca1a2bb7e8a1db2832148ce9933eea0f3ac9548d793112d9a95c9407efad",
		"all hour make first leader extend hole alien behind guard gospel lava path output census museum junior mass reopen famous sing advance salt reform"},
	{"f585c11aec520db57dd353c69554b21a89b20fb0650966fa0a9d6f74fd989d8f",
		"void come effort suffer camp survey warrior heavy shoot primary clutch crush open amazing screen patrol group space point ten exist slush involve unfold"},
}

func TestThePhraseIsTheBIP39MnemonicOfTheSeedAsEntropy(t *testing.T) {
	for _, v := range bip39 {
		k := keyFrom(t, v[0])
		if got := k.Phrase().String(); got != v[1] {
			t.Errorf("the phrase of entropy %s is %q; want %q", v[0], got, v[1])
		}
		back, err := ParsePhrase(v[1])
		if err != nil || back != k {
			t.Errorf("ParsePhrase(%q) = %v, %v; want the key of entropy %s", v[1], back, err, v[0])
		}
	}
	if k, err := ParsePhrase("  " + strings.ReplaceAll(bip39[4][1], " ", "\n\t ") + "\n"); err != nil || k != keyFrom(t, bip39[4][0]) {
		t.Errorf("a phrase spaced by any white space reads as %v, %v", k, err)
	}
}

func TestABadWordOrChecksumIsRefusedAndNoRefusalCarriesItsInput(t *testing.T) {
	ws := strings.Fields(bip39[5][1])
	swapped := append([]string{}, ws...)
	swapped[0], swapped[1] = swapped[1], swapped[0]
	lastChanged := append([]string{}, ws[:23]...)
	lastChanged = append(lastChanged, "abandon")
	for name, phrase := range map[string]string{
		"a word not in the list": strings.Join(append([]string{"panda", "eyebrowz"}, ws[2:]...), " "),
		"two words swapped":      strings.Join(swapped, " "),
		"the last word changed":  strings.Join(lastChanged, " "),
		"23 words":               strings.Join(ws[:23], " "),
		"capitals":               strings.ToUpper(bip39[5][1]),
	} {
		k, err := ParsePhrase(phrase)
		if err == nil {
			t.Errorf("%s reads as %v; want it refused", name, k)
			continue
		}
		for _, w := range strings.Fields(phrase) {
			if len(w) > 3 && strings.Contains(err.Error(), w) {
				t.Errorf("%s: the refusal %q carries the word %q", name, err, w)
			}
		}
	}
	k := keyOf(7)
	file := string(k.FileText())
	seed := strings.TrimSuffix(strings.TrimPrefix(file, "ed25519 "), "\n")
	for name, text := range map[string]string{
		"no newline":     strings.TrimSuffix(file, "\n") + " ",
		"another suite":  "ed25518 " + seed + "\n",
		"padded base64":  "ed25519 " + seed[:42] + "=\n",
		"a bad alphabet": "ed25519 " + seed[:42] + "+\n",
		"empty":          "",
		"twice":          file + file,
	} {
		if got, err := ParseKeyFile([]byte(text)); err == nil {
			t.Errorf("a key file with %s reads as %v; want it refused", name, got)
		} else if strings.Contains(err.Error(), seed[:8]) {
			t.Errorf("a key file with %s is refused with %q, which carries the seed", name, err)
		}
	}
}

// didKeys is Ed25519 seeds and their did:key from W3C's vc-di-eddsa
// specification's key pairs (secretKeyMultibase decoded to its seed).
var didKeys = [][2]string{
	{"c96ef9ea10c5e414c471723aff9de72c35fa5b70fae97e8832ecac7d2e2b8ed6", "did:key:z6MkrJVnaZkeFzdQyMZu1cgjg7k1pZZ6pvBQ7XJPt4swbTQ2"},
	{"47eb3eba85ebb34ff469689dbfcb1b93baa648185ddfb734fff41a403185413b", "did:key:z6MktgKTsu1QhX6QPbyqG6geXdw6FQCZBPq7uQpieWbiQiG7"},
	{"aa19f750f580107930df0f2b0201c0c7f4210b0e2f4a4493f72dac472ae0e551", "did:key:z6MkhWqdDBPojHA7cprTGTt5yHv5yUi1B8cnXn8ReLumkw6E"},
}

func TestThePrincipalIsItsDIDKey(t *testing.T) {
	for _, v := range didKeys {
		k := keyFrom(t, v[0])
		if got := k.Principal().String(); got != v[1] {
			t.Errorf("the principal of seed %s is %s; want %s", v[0], got, v[1])
		}
		p, err := ParsePrincipalID(v[1])
		if err != nil || p != k.Principal() {
			t.Errorf("ParsePrincipalID(%s) = %v, %v; want the seed's principal", v[1], p, err)
		}
	}
	for _, s := range []string{"", "did:key:", "did:key:zz", "did:web:example.com", "did:key:z0OIl",
		"did:key:zQ3shokFTS3brHcDQrn82RUDfCZESWL1ZdCEJwekUDPQiYBme"} { // the last is secp256k1
		if p, err := ParsePrincipalID(s); err == nil {
			t.Errorf("ParsePrincipalID(%q) = %v; want it refused", s, p)
		}
	}
}

func TestTheKeyComesBackFromItsFileAndItsPhrase(t *testing.T) {
	for seed := range byte(20) {
		k := keyOf(seed)
		if back, err := ParseKeyFile(k.FileText()); err != nil || back != k {
			t.Errorf("ParseKeyFile(FileText) = %v, %v; want %v", back, err, k)
		}
		back, err := ParsePhrase(k.Phrase().String())
		if err != nil || back.Principal() != k.Principal() {
			t.Errorf("the phrase regenerates %v, %v; want the principal %v", back, err, k.Principal())
		}
	}
}

func TestAKeyPrintsItsPrincipalNeverItsSeed(t *testing.T) {
	k := keyOf(3)
	secrets := []string{hex.EncodeToString(k.seed[:]), base64.RawURLEncoding.EncodeToString(k.seed[:]), string(k.seed[:])}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%x", "%X", "%q", "%d"} {
		got := fmt.Sprintf(verb, k)
		if !strings.Contains(got, k.Principal().String()) {
			t.Errorf("%s prints %q; want the principal", verb, got)
		}
		for _, s := range secrets {
			if strings.Contains(got, s) {
				t.Errorf("%s prints the seed: %q", verb, got)
			}
		}
	}
	if got := fmt.Sprintf("%v", struct{ K Key }{k}); !strings.Contains(got, k.Principal().String()) {
		t.Errorf("a struct holding a key prints %q; want the principal", got)
	}
}
