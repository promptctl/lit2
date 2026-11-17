package events

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/git"
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
// history (§3.2).
type Transition struct {
	From    Name
	To      Name
	Over    []EventID
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
			parent, err := idBits(r.Bytes())
			made = append(made, func(u wire.Unknown) Placement { return BelowParent{Parent: ItemID{b: parent}, unknown: u} })
			return err
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
	err := readNamed(r, map[uint32]named{1: {&p.Field, "field"}}, map[uint32]*[]EventID{3: &p.Over},
		func(tag uint32, r *wire.Reader) error {
			if tag != 2 {
				return nil
			}
			if p.Value != nil {
				return errors.New("it gives its value twice")
			}
			var err error
			p.Value, err = readValue(r.Bytes())
			return err
		})
	p.unknown = r.Rest()
	if err == nil && p.Value == nil {
		err = errors.New("it has no value")
	}
	if err != nil {
		return nil, fmt.Errorf("a field.set: %w", err)
	}
	return p, nil
}

// member.add = 1:field 2:member · unknown….
func (p MemberAdd) AppendTo(w *wire.Writer) {
	w.Bytes(1, nameWire(p.Field))
	w.Bytes(2, nameWire(p.Member))
	w.Keep(p.unknown)
}

// ReadMemberAdd reads member.add at version 1.
func ReadMemberAdd(r *wire.Reader) (Versioned, error) {
	var p MemberAdd
	err := readNamed(r, map[uint32]named{1: {&p.Field, "field"}, 2: {&p.Member, "member"}}, nil, nil)
	p.unknown = r.Rest()
	if err != nil {
		return nil, fmt.Errorf("a member.add: %w", err)
	}
	return p, nil
}

// member.rm = 1:field 2:member 3:observed (an event id, repeated) · unknown….
func (p MemberRm) AppendTo(w *wire.Writer) {
	w.Bytes(1, nameWire(p.Field))
	w.Bytes(2, nameWire(p.Member))
	WriteIDs(w, 3, p.Observed)
	w.Keep(p.unknown)
}

// ReadMemberRm reads member.rm at version 1.
func ReadMemberRm(r *wire.Reader) (Versioned, error) {
	var p MemberRm
	err := readNamed(r, map[uint32]named{1: {&p.Field, "field"}, 2: {&p.Member, "member"}}, map[uint32]*[]EventID{3: &p.Observed}, nil)
	p.unknown = r.Rest()
	if err != nil {
		return nil, fmt.Errorf("a member.rm: %w", err)
	}
	return p, nil
}

// transition = 1:from 2:to 3:over (an event id, repeated) · unknown….
func (p Transition) AppendTo(w *wire.Writer) {
	w.Bytes(1, nameWire(p.From))
	w.Bytes(2, nameWire(p.To))
	WriteIDs(w, 3, p.Over)
	w.Keep(p.unknown)
}

// ReadTransition reads transition at version 1.
func ReadTransition(r *wire.Reader) (Versioned, error) {
	var p Transition
	err := readNamed(r, map[uint32]named{1: {&p.From, "from"}, 2: {&p.To, "to"}}, map[uint32]*[]EventID{3: &p.Over}, nil)
	p.unknown = r.Rest()
	if err != nil {
		return nil, fmt.Errorf("a transition: %w", err)
	}
	return p, nil
}

// named is one name field of a payload: where it is read into, and what
// a refusal calls it.
type named struct {
	into *Name
	what string
}

// readNamed reads a payload's name fields, each exactly once, and its
// lists of event ids, handing every other field to more: the one reader
// of a payload's names, so a name given twice is refused by every kind
// alike, as readValue refuses a value of two forms `[LAW:single-enforcer]`.
func readNamed(r *wire.Reader, names map[uint32]named, ids map[uint32]*[]EventID, more func(uint32, *wire.Reader) error) error {
	seen := map[uint32]bool{}
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		if n, ok := names[tag]; ok {
			if seen[tag] {
				return fmt.Errorf("it gives its %s twice", n.what)
			}
			seen[tag] = true
			v, err := ParseName(string(r.Bytes()))
			if err != nil {
				return fmt.Errorf("its %s: %w", n.what, err)
			}
			*n.into = v
			return nil
		}
		if list, ok := ids[tag]; ok {
			id, err := ReadEventID(r.Bytes())
			*list = append(*list, id)
			return err
		}
		if more == nil {
			return nil
		}
		return more(tag, r)
	})
	if err != nil {
		return err
	}
	for _, tag := range slices.Sorted(maps.Keys(names)) {
		if !seen[tag] {
			return fmt.Errorf("it names no %s", names[tag].what)
		}
	}
	return nil
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

// workspace.apply = 1:version (the bytes) 2:blob 3:over (an event id,
// repeated) · unknown….
func (p WorkspaceApply) AppendTo(w *wire.Writer) {
	w.Bytes(1, p.version)
	w.Bytes(2, objectIDWire(p.blob, "version's blob"))
	WriteIDs(w, 3, p.Over)
	w.Keep(p.unknown)
}

// ReadWorkspaceApply reads workspace.apply at version 1: the bytes and a
// blob that is git's id for them.
func ReadWorkspaceApply(r *wire.Reader) (Versioned, error) {
	var p WorkspaceApply
	var hasVersion, hasBlob bool
	err := readNamed(r, nil, map[uint32]*[]EventID{3: &p.Over}, func(tag uint32, r *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			p.version, hasVersion = r.Bytes(), true
		case 2:
			p.blob, err = readObjectID(r.Bytes(), "the version's blob")
			hasBlob = true
		}
		return err
	})
	p.unknown = r.Rest()
	switch {
	case err != nil:
	case !hasVersion:
		err = errors.New("it carries no version")
	case !hasBlob:
		err = errors.New("it names no blob")
	case git.HashBlob(p.blob.Format(), p.version) != p.blob:
		err = fmt.Errorf("its blob %s is not git's id for the version it carries", p.blob)
	}
	if err != nil {
		return nil, fmt.Errorf("a workspace.apply: %w", err)
	}
	return p, nil
}

// WriteIDs writes each id as field tag, in order.
func WriteIDs(w *wire.Writer, tag uint32, ids []EventID) {
	for _, id := range ids {
		w.Bytes(tag, id[:])
	}
}
