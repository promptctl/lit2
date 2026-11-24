package events

import (
	"errors"
	"fmt"
	"strings"

	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/git"
)

// Name is a field's, a set's or a list's name: one path segment,
// [a-z][a-z0-9_-]*. Only ParseName makes one, so a name that is not a
// segment is unrepresentable `[LAW:parse-dont-validate]`.
type Name struct{ s string }

// ErrNotAName is a text not a name's grammar, and ErrNotALevel a number
// outside the levels, whether a reader or a writer meets it; the packages
// that read names and levels from a person wrap and name them.
var (
	ErrNotAName  = errors.New("a text outside a name's grammar")
	ErrNotALevel = errors.New("a number outside the levels 1 to 5")
)

// ParseName reads a name.
func ParseName(s string) (Name, error) {
	if s == "" || s[0] < 'a' || s[0] > 'z' || strings.TrimLeft(s, "abcdefghijklmnopqrstuvwxyz0123456789_-") != "" {
		return Name{}, fmt.Errorf("%q is not a name, a lowercase letter, then lowercase letters, digits, _ or -: %w", s, ErrNotAName)
	}
	return Name{s: s}, nil
}

func (n Name) String() string { return n.s }

// nameWire is a name as it is stored. Go lets a caller write a zero Name,
// which no reader reads, so the one writer of a name refuses it before a
// signed event can carry it `[LAW:single-enforcer]`.
func nameWire(n Name) []byte {
	if n.s == "" {
		panic(fmt.Errorf("events: a zero Name, which only ParseName makes: %w", ErrNotAName))
	}
	return []byte(n.s)
}

// MustName is s, which the caller's own parse has already read as a name:
// a literal, or a field's or a column's name a schema holds. A name that
// is not one is the caller's bug, so it panics.
func MustName(s string) Name {
	n, err := ParseName(s)
	if err != nil {
		panic(fmt.Sprintf("events: %q was read as a name: %v", s, err))
	}
	return n
}

// Level is a work item's level, 1 at the bottom (a leaf) to 5 (§3.4).
type Level struct{ n uint8 }

// The levels a workspace may declare.
const (
	leafLevel = 1
	topLevel  = 5
)

// levelWire is a level as it is stored. Go lets a caller write a Level
// outside 1 to 5, which no reader reads, so the one writer of a level
// refuses it before a signed event can carry it `[LAW:single-enforcer]`.
func levelWire(l Level) uint64 {
	if l.n < leafLevel || l.n > topLevel {
		panic(fmt.Errorf("events: level %d is not a level: %w", l.n, ErrNotALevel))
	}
	return uint64(l.n)
}

// ParseLevel reads a level, 1 to 5.
func ParseLevel(n int) (Level, error) {
	if n < leafLevel || n > topLevel {
		return Level{}, fmt.Errorf("level %d is not a level: %w", n, ErrNotALevel)
	}
	return Level{n: uint8(n)}, nil
}

// Int is the level's number, 1 at the leaf.
func (l Level) Int() int { return int(l.n) }

// Below is the level one down, the level of this level's children; a
// leaf has none.
func (l Level) Below() (Level, bool) {
	if l.n <= leafLevel {
		return Level{}, false
	}
	return Level{n: l.n - 1}, true
}

// Placement is where a created item sits in the tree: a top-level item
// states its level, and a child its parent, whose level one down is its
// own, so a child never states one (§3.4) `[LAW:one-source-of-truth]`.
//
//sumtype:decl
type Placement interface{ placement() }

// AtLevel is a top-level item at Level, in the synthetic home of that
// level.
type AtLevel struct {
	Level   Level
	unknown wire.Unknown
}

// BelowParent is a child, under Parent: the link the child renders
// under, never part of its id (§3.2).
type BelowParent struct {
	Parent  ItemID
	unknown wire.Unknown
}

func (AtLevel) placement()     {}
func (BelowParent) placement() {}

// Value is a field's value by its form, never a bare string: prose, which
// concurrent writers may each hold, or a structural value the fold
// settles (charter #5).
//
//sumtype:decl
type Value interface {
	value()
	// wire is the value as it is stored. readValue reads it.
	wire() []byte
}

// Prose is a prose value, opaque below the verbs.
type Prose struct {
	Body    Envelope
	unknown wire.Unknown
}

// Plain is a structural value in its canonical text, as the field's kind
// parsed it at the write.
type Plain struct {
	Canonical string
	unknown   wire.Unknown
}

func (Prose) value() {}
func (Plain) value() {}

// The value's wire fields: 1:prose (an envelope) | 2:plain (its text).
const (
	valueProse = 1
	valuePlain = 2
)

// Wire is the value as it is stored: its envelope.
func (p Prose) wire() []byte {
	var w wire.Writer
	w.Bytes(valueProse, p.Body.wire())
	w.Keep(p.unknown)
	return w.Encoded()
}

// Wire is the value as it is stored: its canonical text.
func (p Plain) wire() []byte {
	var w wire.Writer
	w.Bytes(valuePlain, []byte(p.Canonical))
	w.Keep(p.unknown)
	return w.Encoded()
}

// readValue reads a value's stored form: one of its two forms. The fold
// reads a value to apply it, so one in a form this binary does not know is
// a payload it cannot read: refused, and its event Unread
// (INV:preserve-unknown), never a value of no form. A form's constructor waits for the unknown
// fields, which are only whole once the message is read.
var readValue = declare("a value", []field[func(wire.Unknown) Value]{
	{valueProse, partProse, alternative, func(made *func(wire.Unknown) Value, r *wire.Reader) error {
		e, err := readEnvelope(r.Bytes())
		*made = func(u wire.Unknown) Value { return Prose{Body: e, unknown: u} }
		return err
	}},
	{valuePlain, partPlain, alternative, func(made *func(wire.Unknown) Value, r *wire.Reader) error {
		c := string(r.Bytes())
		*made = func(u wire.Unknown) Value { return Plain{Canonical: c, unknown: u} }
		return nil
	}},
}, func(made *func(wire.Unknown) Value, u wire.Unknown) (Value, error) {
	return (*made)(u), nil
}, forms{what: partValue, card: one}).read

// ItemCreate is item.create: About is the NewItem it creates, and Place
// is the one record of where the item sits.
type ItemCreate struct {
	Place   Placement
	unknown wire.Unknown
}

// FieldSet is field.set: About is the item or element whose field is
// written. Over is the writes the writer read for this field, which this
// one replaces: a write that names none is concurrent with every other.
type FieldSet struct {
	Field   Name
	Value   Value
	Over    []EventID
	unknown wire.Unknown
}

// MemberAdd is member.add: About is the item, and Member joins its set
// Field, such as labels. Every many-valued field is a set of names, so one
// kind serves them all.
type MemberAdd struct {
	Field   Name
	Member  Name
	unknown wire.Unknown
}

// MemberRm is member.rm: About is the item; Observed is the member.add
// events the writer saw, which it removes, so an add it did not see
// survives.
type MemberRm struct {
	Field    Name
	Member   Name
	Observed []EventID
	unknown  wire.Unknown
}

// Transition is transition: About is the item, which moves from column
// From to column To. Over is the writes of its status the writer read, as
// a field.set's Over is; From is the column the writer saw it in, kept as
// history (§3.2). Fields is what the move says about its arrival, such as
// the reason a ticket is closed: part of the one write the move is, so
// whoever may move the item may give them, and the next move replaces
// them.
type Transition struct {
	From    Name
	To      Name
	Over    []EventID
	Fields  []TransitionField
	unknown wire.Unknown
}

// TransitionField is one field a transition carries: its name, under the
// item's status, and its value.
type TransitionField struct {
	Field   Name
	Value   Value
	unknown wire.Unknown
}

// CommentAdd is comment.add: About is the NewElement it creates.
type CommentAdd struct {
	Body    Envelope
	unknown wire.Unknown
}

// WorkspaceApply is workspace.apply: About is the root, and the version
// it applies, the whole bytes of a `.lit/workspace.toml`, becomes the
// model in force (§3.15). Over is the applies the writer saw in force,
// which it replaces, as a field.set's Over is; an apply naming every live
// one resolves a conflict. The blob is git's id for the bytes, so the log
// alone says what is in force; only NewWorkspaceApply and the reader make
// one, and both hash the bytes, so a blob that names other bytes is
// unrepresentable `[LAW:one-source-of-truth]`.
type WorkspaceApply struct {
	version []byte
	blob    git.ObjectID
	Over    []EventID
	unknown wire.Unknown
}

// NewWorkspaceApply is an apply of version, its blob in the code
// repository's object format f, replacing over.
func NewWorkspaceApply(version []byte, f git.ObjectFormat, over []EventID) WorkspaceApply {
	return WorkspaceApply{version: version, blob: git.HashBlob(f, version), Over: over}
}

// Version is the applied version's bytes.
func (p WorkspaceApply) Version() []byte { return p.version }

// Blob is git's id for the version's bytes: the config-ref an event written
// under it names.
func (p WorkspaceApply) Blob() git.ObjectID { return p.blob }

func (ItemCreate) Kind() Kind     { return "item.create" }
func (FieldSet) Kind() Kind       { return "field.set" }
func (MemberAdd) Kind() Kind      { return "member.add" }
func (MemberRm) Kind() Kind       { return "member.rm" }
func (Transition) Kind() Kind     { return "transition" }
func (CommentAdd) Kind() Kind     { return "comment.add" }
func (WorkspaceApply) Kind() Kind { return "workspace.apply" }

func (ItemCreate) Schema() SchemaVersion     { return 1 }
func (FieldSet) Schema() SchemaVersion       { return 1 }
func (MemberAdd) Schema() SchemaVersion      { return 1 }
func (MemberRm) Schema() SchemaVersion       { return 1 }
func (Transition) Schema() SchemaVersion     { return 1 }
func (CommentAdd) Schema() SchemaVersion     { return 1 }
func (WorkspaceApply) Schema() SchemaVersion { return 1 }

// The placement's wire fields: 1:at-level (the level) | 2:below-parent
// (the parent's id).
const (
	placeAtLevel     = 1
	placeBelowParent = 2
)

// item.create = 1:placement · unknown….
func (p ItemCreate) AppendTo(w *wire.Writer) {
	w.Bytes(1, PlacementWire(p.Place))
	w.Keep(p.unknown)
}

// PlacementWire is a placement as it is stored. ReadPlacement reads it.
func PlacementWire(p Placement) []byte {
	var pl wire.Writer
	switch at := p.(type) {
	case AtLevel:
		pl.Uvarint(placeAtLevel, levelWire(at.Level))
		pl.Keep(at.unknown)
	case BelowParent:
		pl.Bytes(placeBelowParent, at.Parent.b[:])
		pl.Keep(at.unknown)
	default:
		// [LAW:no-silent-failure] a create placed nowhere would be signed
		// as a create with no level and no parent
		panic(fmt.Sprintf("events: %T is not a Placement", p))
	}
	return pl.Encoded()
}

// itemCreates is item.create at version 1.
var itemCreates = declare("an item.create", []field[ItemCreate]{
	{1, partPlacement, one, func(p *ItemCreate, r *wire.Reader) (err error) {
		p.Place, err = ReadPlacement(r.Bytes())
		return err
	}},
}, func(p *ItemCreate, u wire.Unknown) (Versioned, error) {
	p.unknown = u
	return *p, nil
})

// ReadItemCreate reads item.create at version 1.
func ReadItemCreate(r *wire.Reader) (Versioned, error) { return itemCreates.decode(r) }

// placements is a placement's stored form: one of a level and below its
// parent. The fold places an item by it, so one in a form this binary does
// not know is a payload it cannot read: refused, and its event Unread
// (INV:preserve-unknown), never an item placed nowhere. A form's constructor waits for the
// unknown fields, which are only whole once the message is read.
var placements = declare("a placement", []field[func(wire.Unknown) Placement]{
	{placeAtLevel, partLevel, alternative, func(made *func(wire.Unknown) Placement, r *wire.Reader) error {
		l, err := ParseLevel(int(min(r.Uvarint(), topLevel+1)))
		*made = func(u wire.Unknown) Placement { return AtLevel{Level: l, unknown: u} }
		return err
	}},
	{placeBelowParent, partParent, alternative, func(made *func(wire.Unknown) Placement, r *wire.Reader) error {
		parent, err := idBits(partParent, r.Bytes())
		*made = func(u wire.Unknown) Placement { return BelowParent{Parent: ItemID{b: parent}, unknown: u} }
		return err
	}},
}, func(made *func(wire.Unknown) Placement, u wire.Unknown) (Placement, error) {
	return (*made)(u), nil
}, forms{what: partPlacement, card: one})

// ReadPlacement reads a placement's stored form.
func ReadPlacement(b []byte) (Placement, error) { return placements.read(b) }

// field.set = 1:field 2:value 3:over (an event id, repeated) · unknown….
func (p FieldSet) AppendTo(w *wire.Writer) {
	w.Bytes(1, nameWire(p.Field))
	w.Bytes(2, p.Value.wire())
	WriteIDs(w, 3, p.Over)
	w.Keep(p.unknown)
}

// fieldSets is field.set at version 1.
var fieldSets = declare("a field.set", []field[FieldSet]{
	nameField(1, partField, one, func(p *FieldSet) *Name { return &p.Field }),
	{2, partValue, one, func(p *FieldSet, r *wire.Reader) (err error) {
		p.Value, err = readValue(r.Bytes())
		return err
	}},
	idsField(3, partOver, many, func(p *FieldSet) *[]EventID { return &p.Over }),
}, func(p *FieldSet, u wire.Unknown) (Versioned, error) {
	p.unknown = u
	return *p, nil
})

// ReadFieldSet reads field.set at version 1.
func ReadFieldSet(r *wire.Reader) (Versioned, error) { return fieldSets.decode(r) }

// member.add = 1:field 2:member · unknown….
func (p MemberAdd) AppendTo(w *wire.Writer) {
	w.Bytes(1, nameWire(p.Field))
	w.Bytes(2, nameWire(p.Member))
	w.Keep(p.unknown)
}

// memberAdds is member.add at version 1.
var memberAdds = declare("a member.add", []field[MemberAdd]{
	nameField(1, partField, one, func(p *MemberAdd) *Name { return &p.Field }),
	nameField(2, partMember, one, func(p *MemberAdd) *Name { return &p.Member }),
}, func(p *MemberAdd, u wire.Unknown) (Versioned, error) {
	p.unknown = u
	return *p, nil
})

// ReadMemberAdd reads member.add at version 1.
func ReadMemberAdd(r *wire.Reader) (Versioned, error) { return memberAdds.decode(r) }

// member.rm = 1:field 2:member 3:observed (an event id, repeated) · unknown….
func (p MemberRm) AppendTo(w *wire.Writer) {
	w.Bytes(1, nameWire(p.Field))
	w.Bytes(2, nameWire(p.Member))
	WriteIDs(w, 3, p.Observed)
	w.Keep(p.unknown)
}

// memberRms is member.rm at version 1.
var memberRms = declare("a member.rm", []field[MemberRm]{
	nameField(1, partField, one, func(p *MemberRm) *Name { return &p.Field }),
	nameField(2, partMember, one, func(p *MemberRm) *Name { return &p.Member }),
	idsField(3, partObserved, many, func(p *MemberRm) *[]EventID { return &p.Observed }),
}, func(p *MemberRm, u wire.Unknown) (Versioned, error) {
	p.unknown = u
	return *p, nil
})

// ReadMemberRm reads member.rm at version 1.
func ReadMemberRm(r *wire.Reader) (Versioned, error) { return memberRms.decode(r) }

// transition = 1:from 2:to 3:over (an event id, repeated) 4:field
// (repeated) · unknown…. A transition with no fields is the bytes it was
// before transitions carried any.
func (p Transition) AppendTo(w *wire.Writer) {
	w.Bytes(1, nameWire(p.From))
	w.Bytes(2, nameWire(p.To))
	WriteIDs(w, 3, p.Over)
	for _, f := range p.Fields {
		w.Bytes(4, f.wire())
	}
	w.Keep(p.unknown)
}

// errFieldOrder is a transition's fields not in strict name order.
var errFieldOrder = errors.New("a transition's fields out of strict name order")

// transitions is transition at version 1. Its fields are in strict name
// order, so a field named twice is refused, since one move says one thing
// about each, and two moves that say the same compare equal.
var transitions = declare("a transition", []field[Transition]{
	nameField(1, partFrom, one, func(p *Transition) *Name { return &p.From }),
	nameField(2, partTo, one, func(p *Transition) *Name { return &p.To }),
	idsField(3, partOver, many, func(p *Transition) *[]EventID { return &p.Over }),
	{4, partCarried, many, func(p *Transition, r *wire.Reader) error {
		f, err := readTransitionField(r.Bytes())
		if err != nil {
			return err
		}
		if n := len(p.Fields); n > 0 && p.Fields[n-1].Field.String() >= f.Field.String() {
			return fmt.Errorf("its field %s follows %s, and a transition gives each field once, in name order: %w", f.Field, p.Fields[n-1].Field, errFieldOrder)
		}
		p.Fields = append(p.Fields, f)
		return nil
	}},
}, func(p *Transition, u wire.Unknown) (Versioned, error) {
	p.unknown = u
	return *p, nil
})

// ReadTransition reads transition at version 1.
func ReadTransition(r *wire.Reader) (Versioned, error) { return transitions.decode(r) }

// transition field = 1:field 2:value · unknown….
func (f TransitionField) wire() []byte {
	var w wire.Writer
	w.Bytes(1, nameWire(f.Field))
	w.Bytes(2, f.Value.wire())
	w.Keep(f.unknown)
	return w.Encoded()
}

// readTransitionField reads one field a transition carries: a name and
// exactly one value.
var readTransitionField = declare("a field", []field[TransitionField]{
	nameField(1, partField, one, func(f *TransitionField) *Name { return &f.Field }),
	{2, partValue, one, func(f *TransitionField, r *wire.Reader) (err error) {
		f.Value, err = readValue(r.Bytes())
		return err
	}},
}, func(f *TransitionField, u wire.Unknown) (TransitionField, error) {
	f.unknown = u
	return *f, nil
}).read

// comment.add = 1:body (an envelope) · unknown….
func (p CommentAdd) AppendTo(w *wire.Writer) {
	w.Bytes(1, p.Body.wire())
	w.Keep(p.unknown)
}

// commentAdds is comment.add at version 1.
var commentAdds = declare("a comment.add", []field[CommentAdd]{
	{1, partCommentBody, one, func(p *CommentAdd, r *wire.Reader) (err error) {
		p.Body, err = readEnvelope(r.Bytes())
		return err
	}},
}, func(p *CommentAdd, u wire.Unknown) (Versioned, error) {
	p.unknown = u
	return *p, nil
})

// ReadCommentAdd reads comment.add at version 1.
func ReadCommentAdd(r *wire.Reader) (Versioned, error) { return commentAdds.decode(r) }

// workspace.apply = 1:version (the bytes) 2:blob 3:over (an event id,
// repeated) · unknown….
func (p WorkspaceApply) AppendTo(w *wire.Writer) {
	w.Bytes(1, p.version)
	w.Bytes(2, objectIDWire(p.blob, "version's blob"))
	WriteIDs(w, 3, p.Over)
	w.Keep(p.unknown)
}

// errBlobNotVersion is an apply whose blob names bytes other than its own.
var errBlobNotVersion = errors.New("a blob that is not git's id for the version it carries")

// workspaceApplies is workspace.apply at version 1: the bytes and a blob
// that is git's id for them.
var workspaceApplies = declare("a workspace.apply", []field[WorkspaceApply]{
	{1, partVersion, one, func(p *WorkspaceApply, r *wire.Reader) error {
		p.version = r.Bytes()
		return nil
	}},
	{2, partBlob, one, func(p *WorkspaceApply, r *wire.Reader) (err error) {
		p.blob, err = readObjectID(r.Bytes(), "the version's blob")
		return err
	}},
	idsField(3, partOver, many, func(p *WorkspaceApply) *[]EventID { return &p.Over }),
}, func(p *WorkspaceApply, u wire.Unknown) (Versioned, error) {
	if git.HashBlob(p.blob.Format(), p.version) != p.blob {
		return nil, fmt.Errorf("its blob %s: %w", p.blob, errBlobNotVersion)
	}
	p.unknown = u
	return *p, nil
})

// ReadWorkspaceApply reads workspace.apply at version 1.
func ReadWorkspaceApply(r *wire.Reader) (Versioned, error) { return workspaceApplies.decode(r) }

// WriteIDs writes each id as field tag, in order.
func WriteIDs(w *wire.Writer, tag uint32, ids []EventID) {
	for _, id := range ids {
		w.Bytes(tag, id[:])
	}
}
