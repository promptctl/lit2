package events

import (
	"bytes"
	"errors"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/panics"
	"github.com/promptctl/lit2/internal/store"
)

// bodyWire is e's body as the record holds it, salt and payload.
func bodyWire(t *testing.T, e Event) []byte {
	t.Helper()
	r, ok := e.Body().(Readable)
	if !ok {
		t.Fatalf("the event's body is %#v; want it readable", e.Body())
	}
	var w wire.Writer
	w.Bytes(1, r.Salt[:])
	w.Bytes(2, payloadWire(r.Payload))
	return w.Encoded()
}

// recordOf is a record of header, signed by sig, holding body, or no body
// when body is nil, as a rewrite or a newer writer makes one.
func recordOf(header []byte, sig Signature, body []byte) store.Record {
	var w wire.Writer
	w.Bytes(recordHeader, header)
	w.Bytes(recordSignature, signatureWire(sig))
	if body != nil {
		w.Bytes(recordBody, body)
	}
	return store.Record(w.Encoded())
}

// edited is header with the field tagged drop removed, if any, and the
// fields add writes after the rest.
func edited(header []byte, drop uint32, add func(*wire.Writer)) []byte {
	r := wire.NewReader(header)
	for tag, ok := r.Next(); ok; tag, ok = r.Next() {
		switch {
		case tag != drop:
		case tag == headerSchema || tag == headerPolicy || tag == headerPosition || tag == headerRecordedAt:
			r.Uvarint()
		default:
			r.Bytes()
		}
	}
	if r.Err() != nil {
		panic(r.Err())
	}
	var w wire.Writer
	w.Keep(r.Rest())
	add(&w)
	return w.Encoded()
}

func none(*wire.Writer) {}

// drafts is n generated drafts: creates of items and their elements, and
// acts on the root, on items and on elements, with prose and tags.
func drafts(r *rand.Rand, n int) []Draft {
	item := anItem
	var ds []Draft
	for range n {
		var tags []string
		for range r.IntN(3) {
			tags = append(tags, words(r, 1))
		}
		payload := note{Text: text(words(r, r.IntN(40))), Tags: tags}
		switch r.IntN(5) {
		case 0:
			ds = append(ds, CreateItem{Payload: ItemCreate{Place: BelowParent{Parent: item}}})
		case 1:
			ds = append(ds, CreateElement{Of: item, Payload: payload})
		case 2:
			ds = append(ds, Act{About: Root{}, Payload: payload})
		case 3:
			ds = append(ds, Act{About: Item{ID: item}, Payload: payload})
		default:
			ds = append(ds, Act{About: Element{Of: item, ID: anElement}, Payload: payload})
		}
	}
	return ds
}

func TestGeneratedEventsDecodeAsMintedAndVerify(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	k := keyOf(1)
	for i := range 100 {
		ds := drafts(r, 1+r.IntN(5))
		m := mint(t, context(byte(i), Frontier{highest: uint64(r.IntN(1000))}, Known{}), k, uint64(i), ds[0], ds[1:]...)
		for j, rec := range records(m) {
			got := decoded(t, here, rec)
			if !reflect.DeepEqual(got, m.Events[j]) {
				t.Fatalf("command %d's event %d decodes as\n%#v\nwant it as minted,\n%#v", i, j, got, m.Events[j])
			}
			if v := suites.Check(got.Header().Attribution().Principal, got.SignedBytes(), got.Signature()); v != Valid {
				t.Fatalf("a minted event's signature is %s", v)
			}
		}
	}
}

func TestAChangedHeaderPrincipalOrSuiteFailsTheSignature(t *testing.T) {
	k := keyOf(1)
	e := mint(t, context(1, Frontier{}, Known{}), k, 1, Act{About: Root{}, Payload: note{Text: text("the header is signed")}}).Events[0]
	// a flip that still decodes reaches the signature check; one that does
	// not is malformed, said why. Some flips must decode, or the check
	// below is never run and the test proves nothing about the signature
	checked := 0
	for i := range len(e.raw) {
		header := slices.Clone(e.raw)
		header[i] ^= 0x01
		switch d := Decode(here, recordOf(header, e.Signature(), bodyWire(t, e))).(type) {
		case Malformed:
			if d.Why == "" {
				t.Errorf("header byte %d changed is malformed with no why", i)
			}
		case Event:
			checked++
			if v := suites.Check(d.Header().Attribution().Principal, d.SignedBytes(), d.Signature()); v == Valid {
				t.Errorf("header byte %d changed, the signature still holds", i)
			}
		default:
			t.Fatalf("header byte %d changed decodes as %#v, neither an event nor malformed", i, d)
		}
	}
	if checked == 0 {
		t.Fatalf("none of the %d header flips decodes to an event, so none reaches the signature check", len(e.raw))
	}
	if v := suites.Check(keyOf(2).Principal(), e.SignedBytes(), e.Signature()); v != Invalid {
		t.Errorf("another principal's check is %s; want invalid", v)
	}
	if v := suites.Check(k.Principal(), e.SignedBytes(), Signature{Suite: 9, Bytes: e.Signature().Bytes}); v != UnknownSuite {
		t.Errorf("a suite this binary lacks checks %s; want unknown_suite", v)
	}
	if v := NewSuites().Check(k.Principal(), e.SignedBytes(), e.Signature()); v != UnknownSuite {
		t.Errorf("with no suites the check is %s; want unknown_suite", v)
	}
	// a second suite that accepts what Ed25519 accepts: a signature claiming
	// it under an Ed25519 principal is invalid, since a signature's suite is
	// its principal's, and the twin's own principal is the control that holds
	both := NewSuites(Ed25519(), twin{})
	claimed := Signature{Suite: twin{}.ID(), Bytes: e.Signature().Bytes}
	if v := both.Check(PrincipalID{suite: twin{}.ID(), key: k.Principal().key}, e.SignedBytes(), claimed); v != Valid {
		t.Fatalf("the twin suite's own principal checks %s; the control must be valid", v)
	}
	if v := both.Check(k.Principal(), e.SignedBytes(), claimed); v != Invalid {
		t.Errorf("a signature of another suite under an Ed25519 principal checks %s; want invalid", v)
	}
}

// twin is a second suite that verifies as Ed25519 does, so only the suite a
// signature names sets it apart.
type twin struct{}

func (twin) ID() SuiteID                          { return SuiteEd25519 + 1 }
func (twin) Verify(key, message, sig []byte) bool { return Ed25519().Verify(key, message, sig) }

func TestANewerRecordKeepsTheFieldsThisBinaryDoesNotKnow(t *testing.T) {
	k := keyOf(1)
	// the newer binary writes a payload with a field this one lacks, an
	// envelope with a field this one lacks, and a header field this one
	// lacks, all signed
	future, err := readEnvelope(append(text("prose").wire(), futureField(9, "envelope's")...))
	if err != nil {
		t.Fatal(err)
	}
	theirs := mint(t, context(1, Frontier{}, Known{}), k, 1, Act{About: Item{ID: anItem},
		Payload: newerNote{Text: future, Tags: []string{"b", "a", "c"}, Mood: "keen"}}).Events[0]
	header := edited(theirs.raw, 0, func(w *wire.Writer) { w.Bytes(20, []byte("header's")) })
	rec := recordOf(header, k.Signer().Sign(signed(header)), bodyWire(t, theirs))

	mine := decoded(t, here, rec)
	if v := suites.Check(k.Principal(), mine.SignedBytes(), mine.Signature()); v != Valid || !bytes.Contains(mine.SignedBytes(), []byte("header's")) {
		t.Errorf("a header with a newer field checks %s; want valid, the field signed", v)
	}
	read, ok := mine.Body().(Readable)
	if !ok {
		t.Fatalf("the newer payload is %#v here; want it readable", mine.Body())
	}
	n := read.Payload.(note)
	if !slices.Equal(n.Tags, []string{"b", "a", "c"}) {
		t.Errorf("the tags read as %v; want them in stored order", n.Tags)
	}
	// this binary changes the payload and writes it back
	n.Tags = append(n.Tags, "d")
	back := mint(t, context(1, Frontier{}.Observe(mine.Header()), Known{}), k, 2, Act{About: mine.Header().About(), Payload: n})
	theirsAgain, ok := decoded(t, ahead, records(back)[0]).Body().(Readable)
	if !ok {
		t.Fatal("the payload written back does not read in the newer binary")
	}
	got := theirsAgain.Payload.(newerNote)
	if got.Mood != "keen" || !slices.Equal(got.Tags, []string{"b", "a", "c", "d"}) || !bytes.Equal(got.Text.wire(), future.wire()) {
		t.Errorf("the newer binary reads back %+v; want its mood, the tags in order and its envelope's field intact", got)
	}
}

// futureField is a LEN field a newer binary writes.
func futureField(tag uint32, s string) []byte {
	var w wire.Writer
	w.Bytes(tag, []byte(s))
	return w.Encoded()
}

func TestAPayloadThisBinaryCannotReadIsUnreadWithItsHeaderWhole(t *testing.T) {
	k := keyOf(1)
	at := Item{ID: anItem}
	// each payload is one a newer or a broken binary wrote
	for _, tc := range []struct {
		name string
		p    Payload
		want UnreadWhy
	}{
		{"a newer kind", newer{}, UnknownKind{}},
		{"a newer schema", laterNote{Text: text("v2")}, NewerSchema{Have: 2, Max: 1}},
		{"an undecodable committed payload", garbled{}, Undecodable{}},
	} {
		m := mint(t, context(1, Frontier{highest: 41}, Known{}), k, 1, Act{About: at, Payload: tc.p})
		e, got := m.Events[0], decoded(t, here, records(m)[0])
		unread, ok := got.Body().(Unread)
		if !ok || reflect.TypeOf(unread.Why) != reflect.TypeOf(tc.want) {
			t.Errorf("%s reads %#v; want unread, %T", tc.name, got.Body(), tc.want)
			continue
		}
		if ns, ok := tc.want.(NewerSchema); ok && unread.Why != ns {
			t.Errorf("%s is %#v; want %#v", tc.name, unread.Why, ns)
		}
		if got.Header() != e.Header() || got.ID() != e.ID() || got.Header().Position().n != 42 ||
			suites.Check(k.Principal(), got.SignedBytes(), got.Signature()) != Valid {
			t.Errorf("%s: the header is %+v; want it whole, at 42, its signature valid", tc.name, got.Header())
		}
	}
}

func TestALaterBinaryReadsAnEarlierVersionThroughItsUpcaster(t *testing.T) {
	e := mint(t, context(1, Frontier{}, Known{}), keyOf(1), 1, Act{About: Root{}, Payload: note{Text: text("written at v1")}}).Events[0]
	read, ok := decoded(t, later, recordOf(e.raw, e.Signature(), bodyWire(t, e))).Body().(Readable)
	if !ok || !reflect.DeepEqual(read.Payload, laterNote{Text: text("written at v1"), Pinned: false}) {
		t.Errorf("the later binary reads a v1 note as %#v; want it upcast to v2", read)
	}
}

func TestARewrittenPayloadKeepsTheSignatureTheIDAndTheName(t *testing.T) {
	k := keyOf(1)
	m := mint(t, context(1, Frontier{}, Known{}), k, 1, CreateElement{Of: anItem, Payload: made{Title: text("a secret title")}})
	e := m.Events[0]
	other := mint(t, context(1, Frontier{}, Known{}), k, 9, Act{About: Root{}, Payload: made{Title: text("something else")}}).Events[0]
	for _, tc := range []struct {
		name string
		body []byte
		want Body
	}{
		{"replaced", bodyWire(t, other), Uncommitted{}},
		{"dropped", nil, Erased{}},
		{"torn", []byte{0xff}, Uncommitted{}},
	} {
		got := decoded(t, here, recordOf(e.raw, e.Signature(), tc.body))
		if got.Body() != tc.want || got.ID() != e.ID() || got.Header().About() != e.Header().About() {
			t.Errorf("a %s body reads %#v, id %s, about %v; want %#v, id %s, about %v", tc.name, got.Body(), got.ID(), got.Header().About(), tc.want, e.ID(), e.Header().About())
		}
		if v := suites.Check(k.Principal(), got.SignedBytes(), got.Signature()); v != Valid {
			t.Errorf("a %s body leaves the signature %s; want valid", tc.name, v)
		}
	}
	forged := decoded(t, here, recordOf(e.raw, keyOf(2).Signer().Sign(e.SignedBytes()), bodyWire(t, e)))
	if forged.ID() == e.ID() {
		t.Error("another signature over one header is the same event id")
	}
	twice := mint(t, context(1, Frontier{}, Known{}), k, 3, Act{About: Root{}, Payload: note{Text: text("same")}}, Act{About: Root{}, Payload: note{Text: text("same")}})
	if twice.Events[0].Header().PayloadHash() == twice.Events[1].Header().PayloadHash() {
		t.Error("two equal payloads hash alike; want the salt to set them apart")
	}
}

func TestARecordMissingWhatAnEventCannotBeWithoutIsMalformed(t *testing.T) {
	k := keyOf(1)
	e := mint(t, context(1, Frontier{}, Known{}), k, 1, Act{About: Root{}, Payload: note{Text: text("x")}}).Events[0]
	// the control: the minted header, re-signed, decodes as its event
	decoded(t, here, recordOf(e.raw, k.Signer().Sign(signed(e.raw)), bodyWire(t, e)))
	noMessage := []byte{0xff, 0xff}
	for name, c := range map[string]struct {
		header []byte
		want   error
	}{
		"no config-ref":                {edited(e.raw, headerConfigRef, none), missing{What: "config-ref"}},
		"no policy version":            {edited(e.raw, headerPolicy, none), missing{What: "policy version"}},
		"no causal position":           {edited(e.raw, headerPosition, none), missing{What: "causal position"}},
		"no payload hash":              {edited(e.raw, headerPayloadHash, none), missing{What: "payload hash"}},
		"policy version 0":             {edited(e.raw, headerPolicy, func(w *wire.Writer) { w.Uvarint(headerPolicy, 0) }), zeroField{What: "policy version"}},
		"causal position 0":            {edited(e.raw, headerPosition, func(w *wire.Writer) { w.Uvarint(headerPosition, 0) }), zeroField{What: "causal position"}},
		"recorded-at 0":                {edited(e.raw, headerRecordedAt, func(w *wire.Writer) { w.Uvarint(headerRecordedAt, 0) }), errNotATime},
		"recorded-at past int64":       {edited(e.raw, headerRecordedAt, func(w *wire.Writer) { w.Uvarint(headerRecordedAt, 1<<63) }), errNotATime},
		"a root that names an element": {edited(e.raw, headerSubject, func(w *wire.Writer) { w.Bytes(headerSubject, futureField(2, "abc")) }), errRootNamesPart},
		"a subject that is no item id": {edited(e.raw, headerSubject, func(w *wire.Writer) { w.Bytes(headerSubject, futureField(1, "not an id")) }), wrongSize{What: "an id", Got: len("not an id"), Want: 16}},
		"bytes that are no message":    {noMessage, wireRefusal(t, noMessage)},
	} {
		if _, err := readHeader(c.header); !errors.Is(err, c.want) {
			t.Errorf("a header with %s reads with %v; want %v", name, err, c.want)
		}
		got := Decode(here, recordOf(c.header, k.Signer().Sign(signed(c.header)), bodyWire(t, e)))
		if d, ok := got.(Malformed); !ok || d.Why == "" {
			t.Errorf("a header with %s decodes as %#v; want it malformed, and why", name, got)
		}
	}
	unsigned := futureField(recordHeader, string(e.raw))
	if _, err := readRecord(unsigned); !errors.Is(err, missing{What: "signature"}) {
		t.Errorf("a record with no signature reads with %v; want no signature", err)
	}
	if _, ok := Decode(here, store.Record(unsigned)).(Malformed); !ok {
		t.Error("a record with no signature decodes; want it malformed")
	}
	if _, err := readRecord(futureField(recordSignature, string(signatureWire(e.Signature())))); !errors.Is(err, missing{What: "header"}) {
		t.Errorf("a record with no header reads with %v; want no header", err)
	}
	// an attribution names its checkout and its principal; the one with both
	// is the control
	attribution := func(checkout, principal bool) []byte {
		var w wire.Writer
		if checkout {
			w.Bytes(1, make([]byte, 16))
		}
		if principal {
			w.Bytes(2, k.Principal().wire())
		}
		return w.Encoded()
	}
	if _, err := readAttribution(attribution(true, true)); err != nil {
		t.Fatalf("an attribution with a checkout and a principal: %v; the control must read", err)
	}
	if _, err := readAttribution(attribution(false, true)); !errors.Is(err, missing{What: "16-byte checkout id"}) {
		t.Errorf("an attribution with no checkout reads with %v; want no checkout id", err)
	}
	if _, err := readAttribution(attribution(true, false)); !errors.Is(err, missing{What: "principal"}) {
		t.Errorf("an attribution with no principal reads with %v; want no principal", err)
	}
}

// wireRefusal is the refusal the wire reader itself gives for b, the
// independent judge of bytes that are no message: the sentinel at the end
// of its error's chain, which a decoder over b must pass through.
func wireRefusal(t *testing.T, b []byte) error {
	t.Helper()
	err := wire.NewReader(b).Each(func(uint32, *wire.Reader) error { return nil })
	if err == nil {
		t.Fatalf("the wire reader reads %x; want it refused", b)
	}
	for errors.Unwrap(err) != nil {
		err = errors.Unwrap(err)
	}
	return err
}

func TestARecordWithNewerRecordAndSignatureFieldsDecodesAsItsEvent(t *testing.T) {
	k := keyOf(1)
	at := Item{ID: anItem}
	e := mint(t, context(1, Frontier{highest: 8}, Known{}), k, 1, Act{About: at, Payload: note{Text: text("x")}}).Events[0]
	var w wire.Writer
	w.Bytes(recordHeader, e.raw)
	w.Bytes(recordSignature, append(signatureWire(e.Signature()), futureField(3, "a newer signature field")...))
	w.Bytes(recordBody, bodyWire(t, e))
	w.Bytes(4, []byte("a newer record field"))
	got := decoded(t, here, store.Record(w.Encoded()))
	if got.Header() != e.Header() || got.Header().Position().n != 9 || got.Header().About() != at {
		t.Errorf("the header reads as %+v; want the minted one, at 9, about %v", got.Header(), at)
	}
	if v := suites.Check(k.Principal(), got.SignedBytes(), got.Signature()); v != Valid {
		t.Errorf("the signature checks %s; want valid", v)
	}
	if _, ok := got.Body().(Readable); !ok {
		t.Errorf("the body is %#v; want it readable", got.Body())
	}
}

func TestMintRefusesWhatACallerLeftUnset(t *testing.T) {
	at := context(1, Frontier{}, Known{})
	act := Act{About: Root{}, Payload: note{Text: text("x")}}
	// the control: each case below differs from it in the one respect named
	mint(t, at, keyOf(1), 1, act)
	mint(t, at, keyOf(1), 1, Act{About: NewItem{ID: anItem, Width: minWidth}, Payload: act.Payload})
	noClock := at
	noClock.Now = time.Time{}
	noAgent := at
	noAgent.Agent = nil
	noPolicy := at
	noPolicy.Policy = 0
	for name, tc := range map[string]struct {
		c    Context
		d    Draft
		want error
	}{
		"an act about nothing":       {at, Act{Payload: act.Payload}, errNotAnAbout},
		"a payload's unset envelope": {at, Act{About: Root{}, Payload: note{}}, errNoForm},
		"no clock reading":           {noClock, act, errNoClock},
		"no agent":                   {noAgent, act, errNotAnAgent},
		"a two-digit segment":        {at, Act{About: NewItem{ID: anItem, Width: 2}, Payload: act.Payload}, errWidth},
		"no policy version":          {noPolicy, act, zeroField{What: "policy version"}},
	} {
		t.Run(name, func(t *testing.T) {
			panics.With(t, tc.want, func() { _, _ = Mint(tc.c, NewDrafts(tc.d), keyOf(1).Signer(), entropy(1)) })
		})
	}
}
