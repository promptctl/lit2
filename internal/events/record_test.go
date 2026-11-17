package events

import (
	"bytes"
	"math/rand/v2"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/events/wire"
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
	for i := range len(e.raw) {
		header := slices.Clone(e.raw)
		header[i] ^= 0x01
		switch d := Decode(here, recordOf(header, e.Signature(), bodyWire(t, e))).(type) {
		case Malformed:
		case Event:
			if v := suites.Check(d.Header().Attribution().Principal, d.SignedBytes(), d.Signature()); v == Valid {
				t.Errorf("header byte %d changed, the signature still holds", i)
			}
		}
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
}

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
	for name, header := range map[string][]byte{
		"no config-ref":                edited(e.raw, headerConfigRef, none),
		"no policy version":            edited(e.raw, headerPolicy, none),
		"no causal position":           edited(e.raw, headerPosition, none),
		"no payload hash":              edited(e.raw, headerPayloadHash, none),
		"policy version 0":             edited(e.raw, headerPolicy, func(w *wire.Writer) { w.Uvarint(headerPolicy, 0) }),
		"causal position 0":            edited(e.raw, headerPosition, func(w *wire.Writer) { w.Uvarint(headerPosition, 0) }),
		"recorded-at 0":                edited(e.raw, headerRecordedAt, func(w *wire.Writer) { w.Uvarint(headerRecordedAt, 0) }),
		"recorded-at past int64":       edited(e.raw, headerRecordedAt, func(w *wire.Writer) { w.Uvarint(headerRecordedAt, 1<<63) }),
		"a root that names an element": edited(e.raw, headerSubject, func(w *wire.Writer) { w.Bytes(headerSubject, futureField(2, "abc")) }),
		"a subject that is no item id": edited(e.raw, headerSubject, func(w *wire.Writer) { w.Bytes(headerSubject, futureField(1, "not an id")) }),
		"bytes that are no message":    {0xff, 0xff},
	} {
		got := Decode(here, recordOf(header, k.Signer().Sign(signed(header)), bodyWire(t, e)))
		if d, ok := got.(Malformed); !ok || d.Why == "" {
			t.Errorf("a header with %s decodes as %#v; want it malformed, and why", name, got)
		}
	}
	if _, ok := Decode(here, store.Record(futureField(recordHeader, string(e.raw)))).(Malformed); !ok {
		t.Error("a record with no signature decodes; want it malformed")
	}
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
	noClock := at
	noClock.Now = time.Time{}
	noAgent := at
	noAgent.Agent = nil
	for name, tc := range map[string]struct {
		c Context
		d Draft
	}{
		"an act about nothing":       {at, Act{Payload: note{Text: text("x")}}},
		"a payload's unset envelope": {at, Act{About: Root{}, Payload: note{}}},
		"no clock reading":           {noClock, Act{About: Root{}, Payload: note{Text: text("x")}}},
		"no agent":                   {noAgent, Act{About: Root{}, Payload: note{Text: text("x")}}},
		"a two-digit segment":        {at, Act{About: NewItem{ID: anItem, Width: 2}, Payload: note{Text: text("x")}}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Mint with %s did not panic", name)
				}
			}()
			_, _ = Mint(tc.c, NewDrafts(tc.d), keyOf(1).Signer(), entropy(1))
		}()
	}
}
