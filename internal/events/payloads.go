package events

import (
	"errors"
	"fmt"
	"strings"

	"github.com/promptctl/lit2/internal/events/wire"
)

// Name is a field's, a set's or a list's name: one path segment,
// [a-z][a-z0-9_-]*. Only ParseName makes one, so a name that is not a
// segment is unrepresentable `[LAW:parse-dont-validate]`.
type Name struct{ s string }

// ParseName reads a name.
func ParseName(s string) (Name, error) {
	if s == "" || s[0] < 'a' || s[0] > 'z' || strings.TrimLeft(s, "abcdefghijklmnopqrstuvwxyz0123456789_-") != "" {
		return Name{}, fmt.Errorf("%q is not a name: a lowercase letter, then lowercase letters, digits, _ or -", s)
	}
	return Name{s: s}, nil
}

func (n Name) String() string { return n.s }

// nameWire is a name as it is stored. Go lets a caller write a zero Name,
// which no reader reads, so the one writer of a name refuses it before a
// signed event can carry it `[LAW:single-enforcer]`.
func nameWire(n Name) []byte {
	if n.s == "" {
		panic("events: a zero Name is not a name: only ParseName makes one")
	}
	return []byte(n.s)
}

// Label is one label of an item: a name, an open set the workspace
// chooses. The labels a workspace file declares and the field table's
// label kind are this type.
type Label struct{ name Name }

// ParseLabel reads a label.
func ParseLabel(s string) (Label, error) {
	n, err := ParseName(s)
	if err != nil {
		return Label{}, fmt.Errorf("a label: %w", err)
	}
	return Label{name: n}, nil
}

func (l Label) String() string { return l.name.s }

// Name is the label as a member of an item's labels.
func (l Label) Name() Name { return l.name }

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
		panic(fmt.Sprintf("events: level %d is not a level: 1 to 5", l.n))
	}
	return uint64(l.n)
}

// ParseLevel reads a level, 1 to 5.
func ParseLevel(n int) (Level, error) {
	if n < leafLevel || n > topLevel {
		return Level{}, fmt.Errorf("level %d is not a level: 1 to 5", n)
	}
	return Level{n: uint8(n)}, nil
}

// Below is the level one down, the level of this level's children; a
// leaf has none.
func (l Level) Below() (Level, bool) {
	if l.n <= leafLevel {
		return Level{}, false
	}
	return Level{n: l.n - 1}, true
}

// Placement is where a created item sits in the tree: a top-level id
// states its level, and a child's level is its parent's one down, so a
// child never states one (§3.4) `[LAW:one-source-of-truth]`.
//
//sumtype:decl
type Placement interface{ placement() }

// AtLevel is a top-level item at Level, in the synthetic home of that
// level.
type AtLevel struct {
	Level   Level
	unknown wire.Unknown
}

// BelowParent is a child, under the item its id names.
type BelowParent struct{ unknown wire.Unknown }

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

// readValue reads a value's stored form: exactly one of the two forms.
func readValue(b []byte) (Value, error) {
	// each form's constructor waits for the unknown fields, which are only
	// whole once the loop has read the message
	var made []func(wire.Unknown) Value
	r := wire.NewReader(b)
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		switch tag {
		case valueProse:
			e, err := readEnvelope(r.Bytes())
			made = append(made, func(u wire.Unknown) Value { return Prose{Body: e, unknown: u} })
			return err
		case valuePlain:
			c := string(r.Bytes())
			made = append(made, func(u wire.Unknown) Value { return Plain{Canonical: c, unknown: u} })
		}
		return nil
	})
	switch {
	case err != nil:
		return nil, fmt.Errorf("a value: %w", err)
	case len(made) != 1:
		return nil, fmt.Errorf("a value is prose or plain; this one is %d of them", len(made))
	}
	return made[0](r.Rest()), nil
}

// ItemCreate is item.create: About is the item it creates.
type ItemCreate struct {
	Place   Placement
	unknown wire.Unknown
}

// ItemDelete is item.delete: About is the item, tombstoned, reversibly
// (§3.2).
type ItemDelete struct{ unknown wire.Unknown }

// FieldSet is field.set: About is the item or element whose field is
// written. Over is the writes the writer read for this field, which this
// one replaces: a write that names none is concurrent with every other.
type FieldSet struct {
	Field   Name
	Value   Value
	Over    []EventID
	unknown wire.Unknown
}

// LabelAdd is label.add: About is the item.
type LabelAdd struct {
	Label   Label
	unknown wire.Unknown
}

// LabelRm is label.rm: About is the item; Observed is the label.add events
// the writer saw, which it removes, so an add it did not see survives.
type LabelRm struct {
	Label    Label
	Observed []EventID
	unknown  wire.Unknown
}

// CommentAdd is comment.add: About is the element it creates, its name
// minted in the item's elements.
type CommentAdd struct {
	Body    Envelope
	unknown wire.Unknown
}

func (ItemCreate) Kind() Kind { return "item.create" }
func (ItemDelete) Kind() Kind { return "item.delete" }
func (FieldSet) Kind() Kind   { return "field.set" }
func (LabelAdd) Kind() Kind   { return "label.add" }
func (LabelRm) Kind() Kind    { return "label.rm" }
func (CommentAdd) Kind() Kind { return "comment.add" }

func (ItemCreate) Schema() SchemaVersion { return 1 }
func (ItemDelete) Schema() SchemaVersion { return 1 }
func (FieldSet) Schema() SchemaVersion   { return 1 }
func (LabelAdd) Schema() SchemaVersion   { return 1 }
func (LabelRm) Schema() SchemaVersion    { return 1 }
func (CommentAdd) Schema() SchemaVersion { return 1 }

// The placement's wire fields: 1:at-level (the level) | 2:below-parent.
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
		pl.Bytes(placeBelowParent, nil)
		pl.Keep(at.unknown)
	default:
		// [LAW:no-silent-failure] a create placed nowhere would be signed
		// as a create with no level and no parent
		panic(fmt.Sprintf("events: %T is not a Placement", p))
	}
	return pl.Encoded()
}

// ReadItemCreate reads item.create at version 1.
func ReadItemCreate(r *wire.Reader) (Versioned, error) {
	var p ItemCreate
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		var err error
		if tag == 1 {
			p.Place, err = ReadPlacement(r.Bytes())
		}
		return err
	})
	p.unknown = r.Rest()
	switch {
	case err != nil:
		return nil, err
	case p.Place == nil:
		return nil, errors.New("an item.create has no placement")
	}
	return p, nil
}

// ReadPlacement reads a placement's stored form: exactly one of a level
// and below its parent.
func ReadPlacement(b []byte) (Placement, error) {
	// each form's constructor waits for the unknown fields, which are only
	// whole once the loop has read the message
	var made []func(wire.Unknown) Placement
	r := wire.NewReader(b)
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		switch tag {
		case placeAtLevel:
			l, err := ParseLevel(int(min(r.Uvarint(), topLevel+1)))
			made = append(made, func(u wire.Unknown) Placement { return AtLevel{Level: l, unknown: u} })
			return err
		case placeBelowParent:
			r.Bytes()
			made = append(made, func(u wire.Unknown) Placement { return BelowParent{unknown: u} })
		}
		return nil
	})
	switch {
	case err != nil:
		return nil, fmt.Errorf("a placement: %w", err)
	case len(made) != 1:
		return nil, fmt.Errorf("a placement is a level or below its parent; this one is %d of them", len(made))
	}
	return made[0](r.Rest()), nil
}

// item.delete = unknown….
func (p ItemDelete) AppendTo(w *wire.Writer) { w.Keep(p.unknown) }

// ReadItemDelete reads item.delete at version 1.
func ReadItemDelete(r *wire.Reader) (Versioned, error) {
	err := r.Each(func(uint32, *wire.Reader) error { return nil })
	return ItemDelete{unknown: r.Rest()}, err
}

// field.set = 1:field 2:value 3:over (an event id, repeated) · unknown….
func (p FieldSet) AppendTo(w *wire.Writer) {
	w.Bytes(1, nameWire(p.Field))
	w.Bytes(2, p.Value.wire())
	WriteIDs(w, 3, p.Over)
	w.Keep(p.unknown)
}

// ReadFieldSet reads field.set at version 1.
func ReadFieldSet(r *wire.Reader) (Versioned, error) {
	var p FieldSet
	var field []byte
	var hasField bool
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			field, hasField = r.Bytes(), true
		case 2:
			p.Value, err = readValue(r.Bytes())
		case 3:
			var id EventID
			id, err = ReadEventID(r.Bytes())
			p.Over = append(p.Over, id)
		}
		return err
	})
	p.unknown = r.Rest()
	switch {
	case err != nil:
		return nil, err
	case !hasField:
		return nil, errors.New("a field.set names no field")
	case p.Value == nil:
		return nil, errors.New("a field.set has no value")
	}
	if p.Field, err = ParseName(string(field)); err != nil {
		return nil, fmt.Errorf("a field.set's field: %w", err)
	}
	return p, nil
}

// label.add = 1:label · unknown….
func (p LabelAdd) AppendTo(w *wire.Writer) {
	w.Bytes(1, nameWire(p.Label.name))
	w.Keep(p.unknown)
}

// ReadLabelAdd reads label.add at version 1.
func ReadLabelAdd(r *wire.Reader) (Versioned, error) {
	var p LabelAdd
	label, err := readLabel(r, func(*wire.Reader, uint32) error { return nil })
	p.Label, p.unknown = label, r.Rest()
	if err != nil {
		return nil, fmt.Errorf("a label.add: %w", err)
	}
	return p, nil
}

// label.rm = 1:label 2:observed (an event id, repeated) · unknown….
func (p LabelRm) AppendTo(w *wire.Writer) {
	w.Bytes(1, nameWire(p.Label.name))
	WriteIDs(w, 2, p.Observed)
	w.Keep(p.unknown)
}

// ReadLabelRm reads label.rm at version 1.
func ReadLabelRm(r *wire.Reader) (Versioned, error) {
	var p LabelRm
	label, err := readLabel(r, func(r *wire.Reader, tag uint32) error {
		if tag != 2 {
			return nil
		}
		id, err := ReadEventID(r.Bytes())
		p.Observed = append(p.Observed, id)
		return err
	})
	p.Label, p.unknown = label, r.Rest()
	if err != nil {
		return nil, fmt.Errorf("a label.rm: %w", err)
	}
	return p, nil
}

// readLabel reads a message whose field 1 is a label, handing every other
// field to more.
func readLabel(r *wire.Reader, more func(*wire.Reader, uint32) error) (Label, error) {
	var label []byte
	var hasLabel bool
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		if tag == 1 {
			label, hasLabel = r.Bytes(), true
			return nil
		}
		return more(r, tag)
	})
	switch {
	case err != nil:
		return Label{}, err
	case !hasLabel:
		return Label{}, errors.New("it names no label")
	}
	return ParseLabel(string(label))
}

// comment.add = 1:body (an envelope) · unknown….
func (p CommentAdd) AppendTo(w *wire.Writer) {
	w.Bytes(1, p.Body.wire())
	w.Keep(p.unknown)
}

// ReadCommentAdd reads comment.add at version 1.
func ReadCommentAdd(r *wire.Reader) (Versioned, error) {
	var p CommentAdd
	var hasBody bool
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		var err error
		if tag == 1 {
			p.Body, err = readEnvelope(r.Bytes())
			hasBody = true
		}
		return err
	})
	p.unknown = r.Rest()
	switch {
	case err != nil:
		return nil, fmt.Errorf("a comment.add: %w", err)
	case !hasBody:
		return nil, errors.New("a comment.add has no body")
	}
	return p, nil
}

// WriteIDs writes each id as field tag, in order.
func WriteIDs(w *wire.Writer, tag uint32, ids []EventID) {
	for _, id := range ids {
		w.Bytes(tag, id[:])
	}
}
