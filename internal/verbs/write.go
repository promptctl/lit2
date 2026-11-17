package verbs

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/promptctl/lit2/internal/address"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/evaluate"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/field"
	"github.com/promptctl/lit2/internal/fold"
)

// Verb is a write verb.
type Verb uint8

const (
	Set Verb = iota + 1
	Add
	Rm
)

func (v Verb) String() string {
	switch v {
	case Set:
		return "set"
	case Add:
		return "add"
	case Rm:
		return "rm"
	}
	panic(fmt.Sprintf("verbs: %d is not a Verb", uint8(v)))
}

// Operand is one operand of a write: a path, and the value given for it,
// which rm has none of.
type Operand struct {
	Path  string
	Value string
}

// Stdin is the operand value that reads standard input.
const Stdin = "-"

// Subject is what a write is about: a node that exists or an item this
// command creates, where it sits, the sections it is read through, and
// what it holds. A created item holds the zero Facts: every register
// unset, every set empty, so a compile function reads both alike.
type Subject struct {
	At    Who
	Place field.Place
	In    config.Ancestry
	Facts fold.Facts
}

// Who is a node a write is about.
//
//sumtype:decl
type Who interface{ who() }

// Existing is a node the state holds, an item or an element of one, as
// its events name it, and its path as it renders.
type Existing struct {
	At       events.About
	Rendered string
}

// Created is the Nth item this command creates, counting from 1 in
// document order.
type Created struct{ N int }

func (Existing) who() {}
func (Created) who()  {}

// Source is where a write's value came from.
//
//sumtype:decl
type Source interface{ source() }

// FromOperand is a value an operand gave.
type FromOperand struct{}

// FromTemplate is a default a level's template gave a new item.
type FromTemplate struct {
	Level string
	Field field.Path
}

func (FromOperand) source()  {}
func (FromTemplate) source() {}

// shape is what a write does, and which one function compiles it.
type shape uint8

const (
	setOne shape = iota + 1
	addMember
	rmMember
	setMembers
	addComment
	// addChild expands to creates and their writes, never to one payload
	addChild
)

// write is the unit of compilation: one value written to one field of one
// subject. Every source of a write, an operand, a child object's key and
// a template's default, makes one, and each shape has one compile
// function, so no rule lives twice.
type write struct {
	subject Subject
	path    field.Path
	decl    field.FieldDecl
	value   field.Literal
	source  Source
	shape   shape
}

// piece is what a compile function returns: a fact about its subject, or
// a new element of it, which Mint names.
//
//sumtype:decl
type piece interface{ piece() }

type fact struct{ payload events.Payload }
type newElement struct{ payload events.Payload }

func (fact) piece()       {}
func (newElement) piece() {}

// claim is what a write overwrites, which one command writes once: a
// whole field, a register or a set replaced; a set's member; or standard
// input. A command has one verb: set claims whole fields, add members, and
// rm a member or the deleted flag, so no command claims a set whole and a
// member of it.
type claim struct {
	who    events.About
	path   field.Path
	member string
}

var stdinClaim = claim{path: "-"}

// twice is the refusal of the operand at path that repeats k.
func (k claim) twice(path string) error {
	if k == stdinClaim {
		return fmt.Errorf("standard input is read once in a command, and %s reads it again", path)
	}
	return fmt.Errorf("%s is written twice in this command", path)
}

// refusal is why an operand compiles to nothing.
type refusal struct {
	reason string
	err    error
	detail string
}

func refuse(reason string, format string, args ...any) *refusal {
	return &refusal{reason: reason, err: fmt.Errorf(format, args...)}
}

// creation is one item an operand creates: its number, where it is
// placed, and the container it is placed in.
type creation struct {
	n         int
	placement events.Placement
	in        fold.Container
}

// expanded is one operand as writes: its address, the items it creates,
// and the writes, in document order.
type expanded struct {
	addr    address.Address
	creates []creation
	writes  []write
	values  int
	stdin   bool
}

// Compile turns a write command into its drafts and changes, or refuses it
// whole. Each operand runs the one pipeline: parse its path, read its value,
// expand it to writes, and compile each write by its shape's function.
// Then one pass in command order refuses a target written twice. No compile
// step mutates state another reads, so a refused operand leaves nothing for
// a later one to trip on. Standard input is read once, by whichever operand
// asks first.
func Compile(w World, verb Verb, operands []Operand, stdin func() (string, error)) Compiled {
	var c Compiled
	created := 0
	read := sync.OnceValues(stdin)
	claims := make([][]claim, len(operands))
	for i, op := range operands {
		var r Result
		r, claims[i] = c.operand(w, verb, i, op, read, &created)
		c.Operands = append(c.Operands, r)
	}
	// [LAW:single-enforcer] the one pass that refuses a repeat
	seen := map[claim]bool{}
	for i, ks := range claims {
		for _, k := range ks {
			if seen[k] {
				c.Operands[i] = Refused{Text: operands[i].Path, Reason: "twice", Err: k.twice(operands[i].Path)}
				break
			}
			seen[k] = true
		}
	}
	if _, refused := c.Refusal(); refused {
		c.Drafts, c.Changes = nil, nil
	}
	return c
}

// attributed is r, a refusal of a write from source, as its operand's: a
// value a level template gave is the template's to answer for, at compile
// and at the gate alike, but for a fault in the configuration, which is
// the configuration's.
func attributed(source Source, r *refusal) *refusal {
	if t, ok := source.(FromTemplate); ok && r.reason != "configuration" {
		return refuse("template", "level %s's template sets %s: %v", t.Level, t.Field, r.err)
	}
	return r
}

// Compiled is a write command compiled:each operand's result, and when no
// operand was refused, the drafts it mints and the changes the echo shows;
// and once Gate has run, the requirements it judged, refused or not, which
// the command event records.
type Compiled struct {
	Operands []Result
	Drafts   []events.Draft
	Changes  []Change
	Tally    Tally
}

// Refusal is the first operand's refusal, the command's, and false when
// none was refused.
func (c Compiled) Refusal() (Refused, bool) { return firstRefused(c.Operands) }

// firstRefused is the first refused result: a command's refusal.
func firstRefused(rs []Result) (Refused, bool) {
	for _, r := range rs {
		if r, ok := r.(Refused); ok {
			return r, true
		}
	}
	return Refused{}, false
}

// Result is one operand's outcome.
//
//sumtype:decl
type Result interface{ result() }

// Accepted is an operand that compiled: its canonical address, how many
// values it gave, and how many drafts it made.
type Accepted struct {
	Text   string
	Canon  string
	Values int
	Drafts int
}

// Refused is an operand that did not: why, and for an address, the
// detail that names its paths. A value's text is never in it.
type Refused struct {
	Text   string
	Reason string
	Err    error
	Detail string
}

func (Accepted) result() {}
func (Refused) result()  {}

// operand compiles one operand: its result, and what it overwrites, which
// the twice pass reads. A refused operand overwrites nothing.
func (c *Compiled) operand(w World, verb Verb, n int, op Operand, stdin func() (string, error), created *int) (Result, []claim) {
	no := func(r *refusal) (Result, []claim) {
		return Refused{Text: op.Path, Reason: r.reason, Err: r.err, Detail: r.detail}, nil
	}
	x, r := expand(w, verb, op, stdin, created)
	if r != nil {
		return no(r)
	}
	var claims []claim
	if x.stdin {
		claims = append(claims, stdinClaim)
	}
	var drafts []events.Draft
	var changes []Change
	then := map[int][]events.Payload{}
	// a created item's changes echo under its create, so they are kept by
	// the item they are about
	about := map[int][]Change{}
	for _, wr := range x.writes {
		pieces, chs, ks, r := compile(w, wr)
		if r != nil {
			return no(attributed(wr.source, r))
		}
		claims = append(claims, ks...)
		for i, ch := range chs {
			if m, ok := ch.(Moved); ok {
				m.Op = n // the operand the gate refuses
				chs[i] = m
			}
		}
		switch who := wr.subject.At.(type) {
		case Existing:
			for _, p := range pieces {
				switch p := p.(type) {
				case fact:
					drafts = append(drafts, events.Act{About: who.At, Payload: p.payload})
				case newElement:
					// only an item's table has a list of elements
					drafts = append(drafts, events.CreateElement{Of: who.At.(events.Item).ID, Payload: p.payload})
				}
			}
		case Created:
			for _, p := range pieces {
				then[who.N] = append(then[who.N], p.(fact).payload)
			}
			about[who.N] = append(about[who.N], chs...)
			continue
		}
		for i, ch := range chs {
			if cm, ok := ch.(Commented); ok {
				cm.Draft = len(drafts) - 1 // the element this write just drafted
				chs[i] = cm
			}
		}
		changes = append(changes, chs...)
	}
	// each create precedes every other draft the operand makes, its own
	// facts in Then, so a new item is named before anything is about it
	base := len(c.Drafts)
	all := make([]events.Draft, 0, len(x.creates)+len(drafts))
	for i, cr := range x.creates {
		all = append(all, events.CreateItem{Payload: events.ItemCreate{Place: cr.placement}, Then: then[cr.n]})
		c.Changes = append(c.Changes, Filed{N: cr.n, Draft: base + i, Parent: w.container(cr.in), Op: n})
		c.Changes = append(c.Changes, about[cr.n]...)
	}
	all = append(all, drafts...)
	for _, ch := range changes {
		if cm, ok := ch.(Commented); ok {
			cm.Draft += base + len(x.creates)
			ch = cm
		}
		c.Changes = append(c.Changes, ch)
	}
	c.Drafts = append(c.Drafts, all...)
	return Accepted{Text: op.Path, Canon: x.addr.String(), Values: x.values, Drafts: len(all)}, claims
}

// expand reads an operand into writes: its path, its value, and then what
// its verb on that target writes.
func expand(w World, verb Verb, op Operand, stdin func() (string, error), created *int) (expanded, *refusal) {
	a, r := address.Parse(w.State(), w.Schema(), op.Path)
	if r != nil {
		return expanded{}, &refusal{reason: r.Reason(), err: r, detail: r.Error()}
	}
	x := expanded{addr: a}
	if verb == Rm {
		return x, x.rm(w)
	}
	t, isField := a.Target.(address.Field)
	if !isField {
		return expanded{}, wrongVerb(verb, "", nil)
	}
	decl, no := fieldOf(t.Row, t.Path)
	if no != nil {
		return expanded{}, no
	}
	sh, ok := shapeOf(verb, decl)
	if !ok {
		return expanded{}, wrongVerb(verb, t.Path, t.Row)
	}
	value, no := x.value(op, stdin, decl)
	if no != nil {
		return expanded{}, no
	}
	x.values = count(value)
	if sh == addChild {
		return x, x.children(w, value, created)
	}
	x.writes = []write{{subject: existing(w, a), path: t.Path, decl: decl, value: value, source: FromOperand{}, shape: sh}}
	return x, nil
}

// fieldOf is the field a row is, which a write takes: a check is the
// engine's verdict, which nothing writes.
func fieldOf(r config.Row, path field.Path) (field.FieldDecl, *refusal) {
	row, isField := r.(config.FieldRow)
	if !isField {
		return field.FieldDecl{}, refuse("derived", "%s is a check: the engine computes it", path)
	}
	return row.Decl, nil
}

// value reads an operand's value. `-` is standard input, which the literal
// grammar never reads: verbatim less one final newline, and for a set one
// member per line, so a list of names pipes in (path-addressed-cli.md
// "Values"). Any other value is read by the literal grammar alone.
func (x *expanded) value(op Operand, stdin func() (string, error), d field.FieldDecl) (field.Literal, *refusal) {
	if op.Value != Stdin {
		v, err := field.ParseLiteral(op.Value)
		if err != nil {
			return nil, refuse("value", "%s: %v", op.Path, err)
		}
		return v, nil
	}
	text, err := stdin()
	if err != nil {
		return nil, refuse("stdin", "reading standard input: %v", err)
	}
	x.stdin = true
	if d.Multi != field.Set {
		return field.Scalar{Text: strings.TrimSuffix(text, "\n")}, nil
	}
	var members []field.Literal
	for line := range strings.Lines(text) {
		members = append(members, field.Scalar{Text: strings.TrimSuffix(line, "\n")})
	}
	return field.List{Items: members}, nil
}

// count is how many values a literal gives.
func count(v field.Literal) int {
	if l, ok := v.(field.List); ok {
		return len(l.Items)
	}
	return 1
}

// rm is the writes of removing the node at x's address: a node's is its
// deleted flag set true, so a node is removable where the table gives it
// one, an item and a comment; and a set's member its removal.
func (x *expanded) rm(w World) *refusal {
	switch t := x.addr.Target.(type) {
	case address.Whole:
		r, err := w.Schema().Table().Find(x.addr.Place, field.Deleted)
		if err != nil {
			return refuse("not_removable", "%s has no deleted flag: the root and a home are the tree's, and rm removes an item, a comment or a member", x.addr)
		}
		x.writes = []write{{subject: existing(w, x.addr), path: field.Deleted, decl: r.(config.FieldRow).Decl, value: field.Scalar{Text: "true"}, source: FromOperand{}, shape: setOne}}
		return nil
	case address.Member:
		x.writes = []write{{subject: existing(w, x.addr), path: t.Path, decl: t.Decl, value: field.Scalar{Text: t.Name.String()}, source: FromOperand{}, shape: rmMember}}
		return nil
	case address.Field:
		return refuse("not_removable", "rm removes an item, a comment or a member, and %s is a field", x.addr)
	}
	panic(fmt.Sprintf("verbs: %T is not a Target", x.addr.Target))
}

// Deletes is whether p deletes a node, an item or a comment: its deleted
// flag set true, the one way rm removes a node.
func Deletes(p events.Payload) bool {
	s, ok := p.(events.FieldSet)
	if !ok || s.Field.String() != string(field.Deleted) {
		return false
	}
	v, plain := s.Value.(events.Plain)
	return plain && v.Canonical == "true"
}

// existing is the subject an address names: a node the state holds.
func existing(w World, a address.Address) Subject {
	at, in, facts := w.held(a.Node)
	return Subject{At: at, Place: a.Place, In: in, Facts: facts}
}

// shapeOf is what verb does to a field of d's shape, and false when it
// does nothing: the one table the compile, a refusal and help all read
// `[LAW:one-source-of-truth]`.
func shapeOf(verb Verb, d field.FieldDecl) (shape, bool) {
	switch {
	case verb == Set && d.Multi == field.One:
		return setOne, true
	case verb == Set && d.Multi == field.Set:
		return setMembers, true
	case verb == Add && d.Multi == field.Set:
		return addMember, true
	case verb == Add && d.Multi == field.Elements:
		return addComment, true
	case verb == Add && d.Multi == field.Children:
		return addChild, true
	}
	return 0, false
}

// VerbsOf is the verbs that write a row, as help and a refusal list them.
// A derived row and a check have none.
func VerbsOf(r config.Row) []string {
	f, ok := r.(config.FieldRow)
	if !ok || field.Writable(f.Decl) != nil {
		return nil
	}
	var out []string
	for _, v := range []Verb{Set, Add} {
		if _, ok := shapeOf(v, f.Decl); ok {
			out = append(out, v.String())
		}
	}
	switch f.Decl.Multi {
	case field.Set:
		out = append(out, "rm "+string(f.At)+"/<member>")
	case field.Elements:
		out = append(out, "rm "+string(f.At)+"/<id>")
	case field.One, field.Children:
	}
	return out
}

func wrongVerb(verb Verb, path field.Path, r config.Row) *refusal {
	if r == nil {
		return refuse("wrong_verb", "%s writes a field, not a whole node or a member; rm removes an item, a comment or a member", verb)
	}
	verbs := VerbsOf(r)
	if len(verbs) == 0 {
		return refuse("derived", "%s: the engine computes it", path)
	}
	return refuse("wrong_verb", "%s does not write %s; its verbs are %s", verb, path, strings.Join(verbs, ", "))
}

// children expands an add to children: each object a create, a write per
// key it gives, and a write per template default it does not.
func (x *expanded) children(w World, v field.Literal, created *int) *refusal {
	var objects []field.Object
	switch v := v.(type) {
	case field.Object:
		objects = []field.Object{v}
	case field.List:
		for _, it := range v.Items {
			o, ok := it.(field.Object)
			if !ok {
				return refuse("value", "a child is an object, {title: …}")
			}
			objects = append(objects, o)
		}
	case field.Scalar:
		return refuse("value", "a child is an object, {title: …}, or a list of them")
	}
	for _, o := range objects {
		*created++
		if r := x.child(w, o, *created); r != nil {
			return r
		}
	}
	return nil
}

// child is one object under x's node: placed by the node it is added to,
// or under / by its level key, which placement reads before any key
// becomes a write.
func (x *expanded) child(w World, o field.Object, n int) *refusal {
	var level events.Level
	var placement events.Placement
	var in fold.Container
	pairs := o.Pairs
	switch node := x.addr.Node.(type) {
	case address.Root:
		level, _ = events.ParseLevel(w.Schema().Levels())
		if i := slices.IndexFunc(pairs, func(p field.Pair) bool { return p.Key == "level" }); i >= 0 {
			s, ok := pairs[i].Value.(field.Scalar)
			if !ok {
				return refuse("value", "level is one value, 1 to %d", w.Schema().Levels())
			}
			text, err := (field.Level{}).Parse(s.Text)
			if err != nil {
				return refuse("value", "level: %v", err)
			}
			num, _ := strconv.Atoi(text)
			l, _ := events.ParseLevel(num)
			if l.Int() > w.Schema().Levels() {
				return refuse("value", "level %d is not declared: this workspace has %d", l.Int(), w.Schema().Levels())
			}
			level = l
			pairs = slices.Delete(slices.Clone(pairs), i, i+1)
		}
		placement, in = events.AtLevel{Level: level}, fold.Home{Holds: level}
	case address.Home:
		level, placement, in = node.Level, events.AtLevel{Level: node.Level}, fold.Home{Holds: node.Level}
	case address.Item:
		above := x.addr.Place.(field.Node).Level
		level, _ = above.Below()
		placement, in = events.BelowParent{Parent: node.ID}, fold.Under{Parent: node.ID}
	case address.Element:
		panic("verbs: a comment's table has no children row")
	}
	sub := Subject{At: Created{N: n}, Place: field.ItemPlace(level), In: evaluate.AncestryOf(w.State(), w.Schema(), in)}
	x.creates = append(x.creates, creation{n: n, placement: placement, in: in})
	given := map[field.Path]bool{}
	for _, p := range pairs {
		path := field.Path(p.Key)
		decl, r := keyed(w, sub.Place, path)
		if r != nil {
			return r
		}
		x.writes = append(x.writes, write{subject: sub, path: path, decl: decl, value: p.Value, source: FromOperand{}, shape: keyShape(decl)})
		given[path] = true
	}
	lv, _ := w.Schema().Level(level.Int())
	for _, path := range slices.Sorted(maps.Keys(lv.Template)) {
		if given[path] {
			continue
		}
		decl, r := keyed(w, sub.Place, path)
		if r != nil {
			return refuse("template", "level %s's template sets %s: %v", lv.Name, path, r.err)
		}
		// a set's default is its members, any number of them, and a
		// register's its one value, which config read as one
		values := lv.Template[path]
		items := make([]field.Literal, len(values))
		for i, s := range values {
			items[i] = field.Scalar{Text: s}
		}
		var v field.Literal = field.List{Items: items}
		if decl.Multi == field.One {
			v = items[0]
		}
		x.writes = append(x.writes, write{subject: sub, path: path, decl: decl, value: v, source: FromTemplate{Level: lv.Name, Field: path}, shape: keyShape(decl)})
		given[path] = true
	}
	if !given[field.Title] {
		return refuse("value", "a new item carries a title, its own or its template's")
	}
	return nil
}

// keyShape is what a key of a child object or a template default writes:
// a register's value or a set's members, the two Keyable admits.
func keyShape(d field.FieldDecl) shape {
	if d.Multi == field.Set {
		return addMember
	}
	return setOne
}

// keyed is the field a child object's key or a template default names at
// a new item's place: found by the table, and one value or a set.
func keyed(w World, p field.Place, path field.Path) (field.FieldDecl, *refusal) {
	r, err := w.Schema().Table().Find(p, path)
	if err != nil {
		var unknown config.UnknownField
		if errors.As(err, &unknown) {
			return field.FieldDecl{}, refuse("unknown_field", "%v", err)
		}
		return field.FieldDecl{}, refuse("not_on_level", "%v", err)
	}
	decl, no := fieldOf(r, path)
	if no != nil {
		return field.FieldDecl{}, no
	}
	if err := field.Keyable(decl); err != nil {
		return field.FieldDecl{}, refuse("value", "%s: %v", path, err)
	}
	return decl, nil
}
