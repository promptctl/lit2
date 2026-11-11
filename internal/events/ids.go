package events

import (
	"fmt"
	"strings"
)

// Prefix is the project prefix every top-level id starts with:
// [a-z][a-z0-9]{2,}, the workspace file's grammar.
type Prefix struct{ s string }

// ParsePrefix reads a project prefix.
func ParsePrefix(s string) (Prefix, error) {
	if len(s) < 3 || s[0] < 'a' || s[0] > 'z' || strings.TrimLeft(s, "abcdefghijklmnopqrstuvwxyz0123456789") != "" {
		return Prefix{}, fmt.Errorf("%q is not a project prefix: a lowercase letter, then two or more lowercase letters or digits", s)
	}
	return Prefix{s: s}, nil
}

func (p Prefix) String() string { return p.s }

// crockford is Crockford's base32 alphabet in lowercase: the digits, then
// the letters without i, l, o and u.
const crockford = "0123456789abcdefghjkmnpqrstvwxyz"

// minWidth is the fewest digits a segment has.
const minWidth = 3

// Segment is one minted name: lowercase Crockford base32 digits, as many
// as Width gave the namespace when it was minted.
type Segment struct{ s string }

// ParseSegment reads one segment of an id.
func ParseSegment(s string) (Segment, error) {
	if len(s) < minWidth || strings.Trim(s, crockford) != "" {
		return Segment{}, fmt.Errorf("%q is not an id segment: %d or more lowercase Crockford base32 digits", s, minWidth)
	}
	return Segment{s: s}, nil
}

func (s Segment) String() string { return s.s }

// ItemID names one item: <prefix>-<segment>, and a child <parent>.<segment>.
// It is comparable, so it keys a map.
type ItemID struct{ s string }

// ParseItemID reads an id in its full form only; a short id is resolved by
// suffix against folded state, which is the address layer's.
func ParseItemID(s string) (ItemID, error) {
	prefix, rest, ok := strings.Cut(s, "-")
	if !ok {
		return ItemID{}, fmt.Errorf("%q is not an item id: no - after its prefix", s)
	}
	if _, err := ParsePrefix(prefix); err != nil {
		return ItemID{}, fmt.Errorf("%q is not an item id: %w", s, err)
	}
	for _, seg := range strings.Split(rest, ".") {
		if _, err := ParseSegment(seg); err != nil {
			return ItemID{}, fmt.Errorf("%q is not an item id: %w", s, err)
		}
	}
	return ItemID{s: s}, nil
}

func (id ItemID) String() string { return id.s }

// Segments are the id's minted names, the top-level one first.
func (id ItemID) Segments() []Segment {
	_, rest, _ := strings.Cut(id.s, "-")
	var ss []Segment
	for _, s := range strings.Split(rest, ".") {
		ss = append(ss, Segment{s: s})
	}
	return ss
}

// Parent is where the id was minted: under its prefix, or under an item.
func (id ItemID) Parent() Parent {
	if at := strings.LastIndexByte(id.s, '.'); at >= 0 {
		return Under{ID: ItemID{s: id.s[:at]}}
	}
	prefix, _, _ := strings.Cut(id.s, "-")
	return Top{Prefix: Prefix{s: prefix}}
}

// Parent is the place a new item's id is minted in.
//
//sumtype:decl
type Parent interface {
	parent()
	// Child is the id of the item named s here.
	Child(s Segment) ItemID
}

// Top is a top-level id's place: under the project prefix.
type Top struct{ Prefix Prefix }

// Under is a child id's place: under its parent item.
type Under struct{ ID ItemID }

func (Top) parent()   {}
func (Under) parent() {}

// Child is <prefix>-<s>.
func (t Top) Child(s Segment) ItemID { return ItemID{s: t.Prefix.s + "-" + s.s} }

// Child is <parent>.<s>.
func (u Under) Child(s Segment) ItemID { return ItemID{s: u.ID.s + "." + s.s} }

// About is what an event is about: the workspace's root, an item, or one
// element of an item, such as a comment or a checklist entry. A root
// carries no element name, since no element namespace exists there
// `[LAW:types-are-the-program]`.
//
//sumtype:decl
type About interface{ about() }

// Root is the workspace's root node.
type Root struct{}

// Item is one item.
type Item struct{ ID ItemID }

// Element is the element named Name in item Of.
type Element struct {
	Of   ItemID
	Name Segment
}

func (Root) about()    {}
func (Item) about()    {}
func (Element) about() {}

// Subject is the node an event concerns, an element's being its item: the
// projection of About the policy reads.
//
//sumtype:decl
type Subject interface{ subject() }

func (Root) subject() {}
func (Item) subject() {}

// subjectOf projects a onto the node it concerns.
func subjectOf(a About) Subject {
	switch a := a.(type) {
	case Root:
		return a
	case Item:
		return a
	case Element:
		return Item{ID: a.Of}
	}
	panic(fmt.Sprintf("events: %T is not an About", a))
}

// Namespace is where a create mints a name: the children of a parent, or
// the elements of an item.
//
//sumtype:decl
type Namespace interface{ namespace() }

// Children is the ids minted under Parent.
type Children struct{ Parent Parent }

// Elements is the names minted for item Of's elements, whatever their kind,
// so an item and segment name exactly one element.
type Elements struct{ Of ItemID }

func (Children) namespace() {}
func (Elements) namespace() {}

// about is what a create in ns, naming s, is about.
func about(ns Namespace, s Segment) About {
	switch ns := ns.(type) {
	case Children:
		return Item{ID: ns.Parent.Child(s)}
	case Elements:
		return Element{Of: ns.Of, Name: s}
	}
	panic(fmt.Sprintf("events: %T is not a Namespace", ns))
}

// Width is the digits a name minted in a namespace already holding taken
// names has: the least w ≥ 3 with 1024·(taken+1) ≤ 32^w, so a new name
// matches a given unseen one with chance at most 1/1024 of the names it
// could be, and the width grows as the namespace fills (3 digits to 31
// names, 4 to 1,023, 5 to 32,767).
func Width(taken int) int {
	w, room := minWidth, uint64(1)<<(5*minWidth)
	for need := 1024 * (uint64(taken) + 1); room < need; w, room = w+1, room<<5 {
	}
	return w
}

// segmentOf is the first w Crockford base32 digits of digest, five bits each,
// most significant first.
func segmentOf(digest [32]byte, w int) Segment {
	if w > 8*len(digest)/5 {
		panic(fmt.Sprintf("events: %d digits is more than a digest holds", w))
	}
	var padded [33]byte // a zero byte past the end, so each digit reads two bytes
	copy(padded[:], digest[:])
	var b strings.Builder
	for i := range w {
		bit := 5 * i
		v := uint16(padded[bit/8])<<8 | uint16(padded[bit/8+1])
		b.WriteByte(crockford[v>>(11-bit%8)&31])
	}
	return Segment{s: b.String()}
}
