package events

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/panics"
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
	// each refusal is built from two phrases, so a refusal that carried any
	// part of its input would differ between them
	phrases := [2]string{bip39[5][1], bip39[6][1]}
	for name, c := range map[string]struct {
		of   func(ws []string) string
		want error
	}{
		"a word not in the list": {func(ws []string) string {
			return strings.Join(append([]string{ws[0], ws[1] + "z"}, ws[2:]...), " ")
		}, errNotAWord},
		"two words swapped": {func(ws []string) string {
			swapped := slices.Clone(ws)
			swapped[0], swapped[1] = swapped[1], swapped[0]
			return strings.Join(swapped, " ")
		}, errChecksum},
		"the last word changed": {func(ws []string) string { return strings.Join(append(slices.Clone(ws[:23]), "abandon"), " ") }, errChecksum},
		"23 words":              {func(ws []string) string { return strings.Join(ws[:23], " ") }, errPhraseLength},
		"capitals":              {func(ws []string) string { return strings.ToUpper(strings.Join(ws, " ")) }, errNotAWord},
	} {
		var said [2]string
		for i, p := range phrases {
			if _, err := ParsePhrase(p); err != nil {
				t.Fatalf("the control phrase %d does not read: %v", i, err)
			}
			phrase := c.of(strings.Fields(p))
			k, err := ParsePhrase(phrase)
			if !errors.Is(err, c.want) {
				t.Errorf("%s reads as %v, %v; want %v", name, k, err, c.want)
				continue
			}
			said[i] = err.Error()
			// past the refusal's own fixed text, no word of the input, of
			// any length, appears
			rest := strings.Replace(said[i], c.want.Error(), "", 1)
			for _, w := range strings.Fields(phrase) {
				if strings.Contains(rest, w) {
					t.Errorf("%s: the refusal %q carries the word %q", name, err, w)
				}
			}
		}
		if said[0] != said[1] {
			t.Errorf("%s: two phrases are refused as %q and %q; want one refusal, carrying neither", name, said[0], said[1])
		}
	}
	files := [2]string{string(keyOf(7).FileText()), string(keyOf(8).FileText())}
	for name, c := range map[string]struct {
		of   func(file, seed string) string
		want error
	}{
		"no newline":     {func(file, _ string) string { return strings.TrimSuffix(file, "\n") + " " }, errKeyFileNewline},
		"another suite":  {func(_, seed string) string { return "ed25518 " + seed + "\n" }, errKeyFileSuite},
		"padded base64":  {func(_, seed string) string { return "ed25519 " + seed[:42] + "=\n" }, errKeyFileSeed},
		"a bad alphabet": {func(_, seed string) string { return "ed25519 " + seed[:42] + "+\n" }, errKeyFileSeed},
		"empty":          {func(string, string) string { return "" }, errKeyFileLength},
		"twice":          {func(file, _ string) string { return file + file }, errKeyFileLength},
	} {
		var said [2]string
		for i, file := range files {
			if _, err := ParseKeyFile([]byte(file)); err != nil {
				t.Fatalf("the control key file %d does not read: %v", i, err)
			}
			seed := strings.TrimSuffix(strings.TrimPrefix(file, "ed25519 "), "\n")
			got, err := ParseKeyFile([]byte(c.of(file, seed)))
			if !errors.Is(err, c.want) {
				t.Errorf("a key file with %s reads as %v, %v; want %v", name, got, err, c.want)
				continue
			}
			said[i] = err.Error()
		}
		// two seeds refused alike: no part of either seed is in the refusal
		if said[0] != said[1] {
			t.Errorf("a key file with %s is refused as %q and %q; want one refusal, carrying neither seed", name, said[0], said[1])
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

// A key file of another suite is refused as one, saying what is wrong with
// it: that it does not start with its suite. The key's own file is the
// control.
func TestAKeyFileOfAnotherSuiteSaysItDoesNotStartWithItsSuite(t *testing.T) {
	file := string(keyOf(7).FileText())
	if _, err := ParseKeyFile([]byte(file)); err != nil {
		t.Fatalf("the control key file does not read: %v", err)
	}
	_, err := ParseKeyFile([]byte("ed25518" + strings.TrimPrefix(file, "ed25519")))
	if !errors.Is(err, errKeyFileSuite) {
		t.Fatalf("a key file of suite ed25518 reads with %v; want errKeyFileSuite", err)
	}
	if !strings.Contains(err.Error(), "does not start with its suite, ed25519") {
		t.Errorf("a key file of another suite is refused as %q; want it to say it does not start with its suite, ed25519", err)
	}
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
	for s, want := range map[string]error{
		"":                    errNotDIDKey,
		"did:web:example.com": errNotDIDKey,
		"did:key:":            errNotDIDKey,
		"did:key:zz":          errNotEd25519Key,
		"did:key:z0OIl":       errNotBase58,
		"did:key:zQ3shokFTS3brHcDQrn82RUDfCZESWL1ZdCEJwekUDPQiYBme": errNotEd25519Key, // secp256k1
	} {
		if p, err := ParsePrincipalID(s); !errors.Is(err, want) {
			t.Errorf("ParsePrincipalID(%q) = %v, %v; want %v", s, p, err, want)
		}
	}
}

func TestTheZeroKeyIsNoKey(t *testing.T) {
	for name, use := range map[string]func(Key){
		"Signer":    func(k Key) { k.Signer() },
		"Principal": func(k Key) { k.Principal() },
		"FileText":  func(k Key) { k.FileText() },
		"Phrase":    func(k Key) { k.Phrase() },
	} {
		t.Run(name, func(t *testing.T) {
			use(keyOf(1)) // the control: a key answers
			panics.With(t, errNoKey, func() { use(Key{}) })
		})
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

func TestAKeyAndItsSignerPrintTheirPrincipalNeverTheSeed(t *testing.T) {
	k := keyOf(3)
	secrets := []string{hex.EncodeToString(k.seed[:]), base64.RawURLEncoding.EncodeToString(k.seed[:]), string(k.seed[:])}
	for name, v := range map[string]any{"a key": k, "its signer": k.Signer()} {
		for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%x", "%X", "%q", "%d"} {
			got := fmt.Sprintf(verb, v)
			if !strings.Contains(got, k.Principal().String()) {
				t.Errorf("%s under %s prints %q; want the principal", name, verb, got)
			}
			for _, s := range secrets {
				if strings.Contains(got, s) {
					t.Errorf("%s under %s prints the seed: %q", name, verb, got)
				}
			}
		}
	}
	held := struct {
		K Key
		S Signer
	}{k, k.Signer()}
	for _, verb := range []string{"%v", "%+v", "%#v"} {
		got := fmt.Sprintf(verb, held)
		if strings.Count(got, k.Principal().String()) != 2 || strings.Contains(got, hex.EncodeToString(k.seed[:])) {
			t.Errorf("a struct holding a key and its signer prints %q under %s; want the principal twice, never the seed", got, verb)
		}
	}
}
