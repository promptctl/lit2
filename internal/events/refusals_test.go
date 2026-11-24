package events

import (
	"encoding/binary"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
)

// span is one field of an encoded message: its tag, where its key starts,
// where its value starts and where the field ends.
type span struct {
	tag               uint32
	start, value, end int
}

// spans is b's fields, cut by protobuf's own key and length rule, so the
// decoder under test is never the judge of where a field lies.
func spans(t *testing.T, b []byte) []span {
	t.Helper()
	var out []span
	for at := 0; at < len(b); {
		start := at
		key, n := binary.Uvarint(b[at:])
		if n <= 0 {
			t.Fatalf("%x: a key at %d is no varint", b, at)
		}
		at += n
		value := at
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
			value = at + n
			at += n + int(size)
		default:
			t.Fatalf("%x: wire type %d at %d", b, key&7, start)
		}
		out = append(out, span{tag: uint32(key >> 3), start: start, value: value, end: at})
	}
	return out
}

// held is how many times b holds the field tagged tag.
func held(t *testing.T, b []byte, tag uint32) int {
	t.Helper()
	n := 0
	for _, s := range spans(t, b) {
		if s.tag == tag {
			n++
		}
	}
	return n
}

// firstOf is b's first field tagged tag.
func firstOf(t *testing.T, b []byte, tag uint32) span {
	t.Helper()
	for _, s := range spans(t, b) {
		if s.tag == tag {
			return s
		}
	}
	t.Fatalf("%x holds no field %d", b, tag)
	return span{}
}

// repeated is b with a copy of its first field tagged tag appended: the
// field given twice.
func repeated(t *testing.T, b []byte, tag uint32) []byte {
	t.Helper()
	f := firstOf(t, b, tag)
	return append(append([]byte(nil), b...), b[f.start:f.end]...)
}

// omitted is b without its fields tagged tag.
func omitted(t *testing.T, b []byte, tag uint32) []byte {
	t.Helper()
	var out []byte
	for _, s := range spans(t, b) {
		if s.tag != tag {
			out = append(out, b[s.start:s.end]...)
		}
	}
	return out
}

// nested is the value of b's first field tagged tag: a message an encoder
// writes inside another.
func nested(t *testing.T, b []byte, tag uint32) []byte {
	t.Helper()
	f := firstOf(t, b, tag)
	return b[f.value:f.end]
}

// controls is each declared message as its writers encode it, by the
// message's name: the oracle of how many times each field is held,
// independent of the declaration. A field every control holds once is one
// its writer always writes; one some control lacks, the writer may leave
// out; one a control holds twice is a list. Each is an encoder's output,
// or, for a record a rewrite erased, the record as the format says it is.
func controls(t *testing.T) map[string][][]byte {
	k := keyOf(1)
	e := mint(t, context(1, Frontier{}, Known{}), k, 1, Act{About: Root{}, Payload: note{Text: text("x")}}).Events[0]
	a, b := keyOf(1).Principal(), keyOf(2).Principal()
	owner, contributor, observer := MustName("owner"), MustName("contributor"), MustName("observer")
	r := e.Body().(Readable)
	record := recordWire(e.raw, signatureWire(e.Signature()), r.Salt, payloadWire(r.Payload))
	sealedEnvelope := Envelope{
		form:       sealed{suite: SuiteEd25519, tier: "tier", epoch: 1, nonce: []byte("nonce"), ciphertext: []byte("ciphertext")},
		commitment: Commitment{9},
	}.wire()
	value := TransitionField{Field: MustName("reason"), Value: Plain{Canonical: "x"}}
	over := []EventID{{1}, {2}}
	enroll := payloadBytes(Enroll{Owner: owner, Roles: map[Name][]Capability{
		contributor: {MustCapability("set own/*"), MustCapability("rm own/* (level=1)")},
		observer:    nil,
	}})
	propose := func(th Threshold) []byte {
		return payloadBytes(Propose{Base: Digest{3}, Members: map[PrincipalID]Name{a: owner, b: contributor}, Threshold: th})
	}
	return map[string][][]byte{
		"the record":    {record, []byte(recordOf(e.raw, e.Signature(), nil))},
		"the signature": {signatureWire(e.Signature())},
		"the body":      {nested(t, record, recordBody)},
		"the header":    {e.raw},
		"the subject":   {aboutWire(NewElement{Of: anItem, ID: anElement, Width: 4}), aboutWire(Root{})},
		"the attribution": {
			Attribution{Checkout: store.CheckoutID{1}, Principal: a, Agent: AgentFrom("x")}.wire(),
			Attribution{Checkout: store.CheckoutID{1}, Principal: a, Agent: Unlabeled{}}.wire(),
		},
		"the principal":     {a.wire()},
		"an object id":      {objectIDWire(git.HashBlob(git.SHA1, aVersion), "blob")},
		"an envelope":       {SealPlain("x").wire(), sealedEnvelope},
		"a plaintext form":  {nested(t, SealPlain("x").wire(), envelopePlain)},
		"a sealed form":     {nested(t, sealedEnvelope, envelopeSealed)},
		"a value":           {Plain{Canonical: "b"}.wire(), Prose{Body: text("a")}.wire()},
		"a placement":       {PlacementWire(AtLevel{Level: mustLevel(1)}), PlacementWire(BelowParent{Parent: anItem})},
		"an item.create":    {payloadBytes(ItemCreate{Place: BelowParent{Parent: anItem}})},
		"a field.set":       {payloadBytes(FieldSet{Field: MustName("title"), Value: Plain{Canonical: "x"}, Over: over})},
		"a member.add":      {payloadBytes(MemberAdd{Field: labels, Member: MustName("a")})},
		"a member.rm":       {payloadBytes(MemberRm{Field: labels, Member: MustName("a"), Observed: over})},
		"a transition":      {payloadBytes(Transition{From: MustName("plan"), To: MustName("done"), Over: over, Fields: []TransitionField{value, {Field: MustName("why"), Value: Plain{Canonical: "y"}}}})},
		"a field":           {value.wire()},
		"a comment.add":     {payloadBytes(CommentAdd{Body: text("c")})},
		"a workspace.apply": {payloadBytes(NewWorkspaceApply(aVersion, git.SHA1, over))},
		"a policy.enroll":   {enroll},
		"a role":            {nested(t, enroll, 2)},
		"a policy.propose":  {propose(Majority{})},
		"a member":          {nested(t, propose(Majority{}), 2)},
		"a threshold":       {nested(t, propose(Majority{}), 3), nested(t, propose(Fixed{N: 1}), 3)},
		"a policy.approve":  {payloadBytes(Approve{Proposal: Digest{1}})},
		"a policy.request":  {payloadBytes(Request{})},
	}
}

// newerForms is each message whose writer writes back a form a newer binary
// wrote, as it writes it: the oracle of which sums admit a form this
// binary does not know. A message absent here is one every writer writes
// in a form this binary knows, since its type has no other.
func newerForms(t *testing.T) map[string][]byte {
	t.Helper()
	r := wire.NewReader(futureField(9, "a newer binary's form"))
	if err := r.Each(func(uint32, *wire.Reader) error { return nil }); err != nil {
		t.Fatalf("a newer binary's form: %v", err)
	}
	return map[string][]byte{
		"an envelope": Envelope{form: unknownForm{}, commitment: Commitment{9}, unknown: r.Rest()}.wire(),
	}
}

// Every sum is read by its declaration alone, against what its writers
// write: a control in one form joined by another form is refused as the
// sum in more than one form; a control in none of its forms is refused as
// no sum; and beside a field this binary does not know, it reads where a
// writer writes a newer binary's form back, and is refused where none
// does. The rows come from the declarations, so a sum added to one is held
// to its writers with no edit here, and a sum declared to admit what no
// writer writes, or refuse what one does, fails a row.
func TestEverySumHoldsOneFormOfIt(t *testing.T) {
	cs, ns := controls(t), newerForms(t)
	for _, d := range declarations {
		nc, admitsNewer := ns[d.name]
		if admitsNewer {
			if err := d.read(nc); err != nil {
				t.Errorf("%s as a newer binary writes it reads with %v; want it read", d.name, err)
			}
		}
		for _, s := range d.sums {
			// declare refuses a sum of fewer than two forms, so s.tags, read
			// off the fields declared alternative, has two to join
			holding := map[uint32][]byte{}
			whole := true
			for _, tag := range s.tags {
				for _, c := range cs[d.name] {
					if held(t, c, tag) > 0 && holding[tag] == nil {
						holding[tag] = c
					}
				}
				if holding[tag] == nil {
					t.Errorf("no control of %s holds its %s's form %d", d.name, s.what, tag)
					whole = false
				}
			}
			if !whole {
				continue
			}
			a, b := s.tags[0], s.tags[1]
			other := holding[b]
			both := append(append([]byte(nil), holding[a]...), other[firstOf(t, other, b).start:firstOf(t, other, b).end]...)
			if err := d.read(both); !errors.Is(err, notOneForm{What: s.what}) {
				t.Errorf("%s in its forms %d and %d reads with %v; want its %s in more than one form", d.name, a, b, err, s.what)
			}
			bare := holding[a]
			for _, tag := range s.tags {
				bare = omitted(t, bare, tag)
			}
			if err := d.read(bare); !errors.Is(err, missing{What: s.what}) {
				t.Errorf("%s in none of its forms reads with %v; want no %s", d.name, err, s.what)
			}
			unknown := slices.MaxFunc(d.fields, func(p, q shape) int { return int(p.tag) - int(q.tag) }).tag + 1
			beside := append(bare, futureField(unknown, "a newer binary's form")...)
			switch err := d.read(beside); {
			case admitsNewer && err != nil:
				t.Errorf("%s in no form it knows, beside field %d, reads with %v; want it read, as its writer writes a newer form back", d.name, unknown, err)
			case !admitsNewer && !errors.Is(err, missing{What: s.what}):
				t.Errorf("%s in no form it knows, beside field %d, reads with %v; want no %s, as no writer writes a newer form", d.name, unknown, err, s.what)
			}
		}
	}
	for n := range ns {
		if !slices.ContainsFunc(declarations, func(d declaration) bool { return d.name == n && len(d.sums) > 0 }) {
			t.Errorf("a newer binary's form is written for %s, which no declaration holds a sum of", n)
		}
	}
}

// Every declared message is read by its declaration alone: each control
// reads; a field its writers hold at most once is refused, given twice, as
// that field given twice; a field its writers always write is refused,
// left out, as that field missing; and the rows come from the declarations,
// so a field added to one is held to both with no edit here. Where the
// declaration says other than its writers do, a row fails: a field it calls
// optional that every writer writes reads with the field left out, and a
// list it calls singular refuses its own control.
func TestEveryDecoderHoldsEachFieldToItsDeclaration(t *testing.T) {
	cs := controls(t)
	for _, d := range declarations {
		ctl, ok := cs[d.name]
		if !ok {
			t.Errorf("%s is declared and has no control", d.name)
			continue
		}
		for i, c := range ctl {
			if err := d.read(c); err != nil {
				t.Fatalf("%s's control %d reads with %v; the control must read", d.name, i, err)
			}
			for _, s := range spans(t, c) {
				if !slices.ContainsFunc(d.fields, func(f shape) bool { return f.tag == s.tag }) {
					t.Errorf("%s's writer writes field %d, which its declaration does not hold", d.name, s.tag)
				}
			}
		}
		for i := 1; i < len(d.fields); i++ {
			if p, q := d.fields[i-1], d.fields[i]; p.tag > q.tag || p.what >= q.what {
				t.Errorf("%s declares field %d as %s and field %d as %s; want the parts in its tags' order", d.name, p.tag, p.what, q.tag, q.what)
			}
		}
		for _, f := range d.fields {
			var holding []byte
			always, list := true, false
			for _, c := range ctl {
				n := held(t, c, f.tag)
				if n > 0 && holding == nil {
					holding = c
				}
				always, list = always && n == 1, list || n > 1
			}
			switch {
			case holding == nil:
				t.Errorf("no control of %s holds its field %d, %s", d.name, f.tag, f.what)
				continue
			case list:
				continue
			}
			if err := d.read(repeated(t, holding, f.tag)); !errors.Is(err, twice{What: f.what}) {
				t.Errorf("%s giving field %d twice reads with %v; want its %s given twice", d.name, f.tag, err, f.what)
			}
			if !always {
				continue
			}
			if err := d.read(omitted(t, holding, f.tag)); !errors.Is(err, missing{What: f.what}) {
				t.Errorf("%s without field %d reads with %v; want no %s, which its writer always writes", d.name, f.tag, err, f.what)
			}
		}
	}
	for n := range cs {
		if !slices.ContainsFunc(declarations, func(d declaration) bool { return d.name == n }) {
			t.Errorf("a control names %s, which no declaration does", n)
		}
	}
}

// A list keyed by its elements, an enrollment's roles or a proposal's
// members, refuses one key twice as its element given twice, and says
// which; the list once is the control.
func TestAListRefusesOneKeyTwice(t *testing.T) {
	a := keyOf(1).Principal()
	owner, contributor := MustName("owner"), MustName("contributor")
	for _, c := range []struct {
		name string
		read func(*wire.Reader) (Versioned, error)
		b    []byte
		want part
		says string
	}{
		{"an enrollment", ReadEnroll, payloadBytes(Enroll{Owner: owner, Roles: map[Name][]Capability{contributor: nil}}), partRole, "contributor"},
		{"a proposal", ReadPropose, payloadBytes(Propose{Base: Digest{3}, Members: map[PrincipalID]Name{a: owner}, Threshold: Majority{}}), partMember, a.String()},
	} {
		if _, err := c.read(wire.NewReader(c.b)); err != nil {
			t.Fatalf("%s whole: %v; the control must read", c.name, err)
		}
		_, err := c.read(wire.NewReader(repeated(t, c.b, 2)))
		if !errors.Is(err, twice{What: c.want}) {
			t.Errorf("%s listing one key twice reads with %v; want its %s given twice", c.name, err, c.want)
			continue
		}
		if !strings.Contains(err.Error(), c.says) {
			t.Errorf("%s listing one key twice is refused as %q; want it to name %s", c.name, err, c.says)
		}
	}
}

// No decoder reads a message but by a declaration: in the package's code,
// only the generic decoder walks a message's fields, and only declare
// builds a message, which it registers, so every decoder is one the
// tests above hold to its declaration.
func TestEveryDecoderReadsByADeclaration(t *testing.T) {
	fset := token.NewFileSet()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	walked, built := 0, 0
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			fn, _ := decl.(*ast.FuncDecl)
			in := "a package variable"
			if fn != nil {
				in = fn.Name.Name
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch n := n.(type) {
				case *ast.CallExpr:
					sel, ok := n.Fun.(*ast.SelectorExpr)
					if ok && (sel.Sel.Name == "Each" || sel.Sel.Name == "Next" && len(n.Args) == 0) {
						walked++
						if in != "decode" {
							t.Errorf("%s: %s walks a message's fields with %s, not by a declaration", fset.Position(n.Pos()), in, sel.Sel.Name)
						}
					}
				case *ast.CompositeLit:
					if x, ok := n.Type.(*ast.IndexListExpr); ok && isIdent(x.X, "message") {
						built++
						if in != "declare" {
							t.Errorf("%s: %s builds a message declare does not register", fset.Position(n.Pos()), in)
						}
					}
				}
				return true
			})
		}
	}
	if walked != 1 || built != 1 {
		t.Errorf("the package walks fields %d times and builds a message %d times; want once each, in decode and declare", walked, built)
	}
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// A body that does not read is refused by the body's own decoder, which
// names why, and the record stands with its signed header, its body
// uncommitted as a rewritten one is: the verdict does not carry the cause,
// and a torn write or damage costs the payload, never the event. The body
// whole is the control, which reads.
func TestABodyThatDoesNotReadIsUncommitted(t *testing.T) {
	k := keyOf(1)
	e := mint(t, context(1, Frontier{}, Known{}), k, 1, Act{About: Root{}, Payload: note{Text: text("x")}}).Events[0]
	body := bodyWire(t, e)
	if got, ok := decoded(t, here, recordOf(e.raw, e.Signature(), body)).Body().(Readable); !ok {
		t.Fatalf("the body whole reads as %#v; the control must read", got)
	}
	if _, err := readBody(body); err != nil {
		t.Fatalf("control: the body whole is refused with %v", err)
	}
	torn := []byte{0xff}
	for name, c := range map[string]struct {
		body []byte
		want error
	}{
		"giving its salt twice":    {repeated(t, body, 1), twice{What: partSalt}},
		"giving its payload twice": {repeated(t, body, 2), twice{What: partPayload}},
		"without its payload":      {omitted(t, body, 2), missing{What: partPayload}},
		"with a short salt":        {append(futureField(1, "short"), body[firstOf(t, body, 2).start:]...), wrongSize{What: partSalt}},
		"torn":                     {torn, wireRefusal(t, torn)},
	} {
		if _, err := readBody(c.body); !errors.Is(err, c.want) {
			t.Errorf("a body %s reads with %v; want %v", name, err, c.want)
			continue
		}
		got := decoded(t, here, recordOf(e.raw, e.Signature(), c.body))
		if got.Body() != (Uncommitted{}) || got.ID() != e.ID() || got.Header().About() != e.Header().About() {
			t.Errorf("a record whose body is %s decodes as %#v; want the event, its body uncommitted", name, got.Body())
		}
	}
}
