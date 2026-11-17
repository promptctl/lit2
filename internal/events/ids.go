package events

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// Prefix is the project prefix ids render under: [a-z][a-z0-9]{2,}, the
// workspace file's grammar. It is a label of the active version, never
// part of an id (§3.2).
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

// minWidth is the fewest digits a segment has; fullWidth is the digits of
// a whole id: 128 bits, five to a digit, the last digit's two low bits
// zero.
const (
	minWidth  = 3
	fullWidth = 26
)

// ItemID names one item: 128 random bits minted at its create and carried
// in it, so where the create lands is no part of the item's identity
// (§3.2). It is comparable, so it keys a map.
type ItemID struct{ b [16]byte }

// ElementID names one element of an item, such as a comment, the same way.
type ElementID struct{ b [16]byte }

// String is the full id: 26 Crockford digits.
func (id ItemID) String() string { return digits(id.b, fullWidth) }

// String is the full id: 26 Crockford digits.
func (id ElementID) String() string { return digits(id.b, fullWidth) }

// Segment is the id's first w digits: its short form at width w.
func (id ItemID) Segment(w int) Segment { return Segment{s: digits(id.b, w)} }

// Segment is the id's first w digits: its short form at width w.
func (id ElementID) Segment(w int) Segment { return Segment{s: digits(id.b, w)} }

// ParseItemID reads an item's full id, the 26 digits String writes.
func ParseItemID(s string) (ItemID, error) {
	b, err := parseFull(s)
	if err != nil {
		return ItemID{}, fmt.Errorf("%q is not an item id: %w", s, err)
	}
	return ItemID{b: b}, nil
}

// parseFull reads 26 Crockford digits whose last two bits are zero: the
// one text of 128 bits.
func parseFull(s string) ([16]byte, error) {
	var b [16]byte
	if len(s) != fullWidth || strings.Trim(s, crockford) != "" {
		return b, fmt.Errorf("a full id is %d lowercase Crockford base32 digits", fullWidth)
	}
	var acc uint32
	var held, at int
	for i := range len(s) {
		acc = acc<<5 | uint32(strings.IndexByte(crockford, s[i]))
		held += 5
		if held >= 8 {
			held -= 8
			b[at] = byte(acc >> held)
			at++
		}
	}
	if acc&(1<<held-1) != 0 {
		return [16]byte{}, fmt.Errorf("a full id's last digit holds three bits and two zeros")
	}
	return b, nil
}

// digits is the first w Crockford digits of b, five bits each, most
// significant first.
func digits(b [16]byte, w int) string {
	if w < 1 || w > fullWidth {
		panic(fmt.Sprintf("events: %d digits of an id that holds %d", w, fullWidth))
	}
	var padded [17]byte // a zero byte past the end, so each digit reads two bytes
	copy(padded[:], b[:])
	out := make([]byte, w)
	for i := range w {
		bit := 5 * i
		v := uint16(padded[bit/8])<<8 | uint16(padded[bit/8+1])
		out[i] = crockford[v>>(11-bit%8)&31]
	}
	return string(out)
}

// sharedDigits is how many leading digits a and b have in common.
func sharedDigits(a, b [16]byte) int {
	bits := 0
	for i := range a {
		if x := a[i] ^ b[i]; x != 0 {
			for x&0x80 == 0 {
				x <<= 1
				bits++
			}
			return bits / 5
		}
		bits += 8
	}
	return fullWidth
}

// Segment is an id's short form: its first digits, as many as its create
// fixed at mint, which is how an id is rendered (§3.2).
type Segment struct{ s string }

// ParseSegment reads one segment.
func ParseSegment(s string) (Segment, error) {
	if len(s) < minWidth || len(s) > fullWidth || strings.Trim(s, crockford) != "" {
		return Segment{}, fmt.Errorf("%q is not an id segment: %d to %d lowercase Crockford base32 digits", s, minWidth, fullWidth)
	}
	return Segment{s: s}, nil
}

func (s Segment) String() string { return s.s }

// Width is the digits a name minted in a namespace already holding fill
// names has at least: the least w ≥ 3 with 1024·(fill+1) ≤ 32^w, so a
// person reading the namespace's segments meets one twice with chance at
// most 1/1024, and the width grows as the namespace fills (3 digits to 31
// names, 4 to 1,023, 5 to 32,767).
func Width(fill int) int {
	w, room := minWidth, uint64(1)<<(5*minWidth)
	for need := 1024 * (uint64(fill) + 1); room < need; w, room = w+1, room<<5 {
	}
	return w
}

// Namespace is the names a segment's width is counted against: every
// top-level item, at any level; the children of one item; or the elements
// of one item.
//
//sumtype:decl
type Namespace interface{ namespace() }

// Top is the top-level items, of every level.
type Top struct{}

// ChildOf is the children of Parent.
type ChildOf struct{ Parent ItemID }

// ElementOf is the elements of Item, whatever their kind.
type ElementOf struct{ Item ItemID }

func (Top) namespace()       {}
func (ChildOf) namespace()   {}
func (ElementOf) namespace() {}

// Known is what a writer has folded that a mint must not collide with:
// every id, item or element, and each namespace's fill. Read-only to Mint.
type Known struct {
	ids  [][16]byte // sorted, so an id's nearest are its neighbours
	fill map[Namespace]int
}

// Fill is how many created names ns holds.
func (k Known) Fill(ns Namespace) int { return k.fill[ns] }

// Holds is whether id is known.
func (k Known) Holds(id ID) bool {
	_, found := slices.BinarySearchFunc(k.ids, id.bits(), compareBits)
	return found
}

// shared is the most leading digits id has in common with a known id.
func (k Known) shared(id [16]byte) int {
	at, _ := slices.BinarySearchFunc(k.ids, id, compareBits)
	most := 0
	for _, i := range []int{at - 1, at} {
		if i >= 0 && i < len(k.ids) {
			most = max(most, sharedDigits(id, k.ids[i]))
		}
	}
	return most
}

func compareBits(a, b [16]byte) int { return bytes.Compare(a[:], b[:]) }

// KnownBuilder is the fold's, fed with every id and create it folds.
type KnownBuilder struct {
	ids  map[[16]byte]struct{}
	fill map[Namespace]int
}

// Saw records an id the fold met, created or only named.
func (b *KnownBuilder) Saw(id ID) {
	if b.ids == nil {
		b.ids = map[[16]byte]struct{}{}
	}
	b.ids[id.bits()] = struct{}{}
}

// Created counts one created name in ns.
func (b *KnownBuilder) Created(ns Namespace) {
	if b.fill == nil {
		b.fill = map[Namespace]int{}
	}
	b.fill[ns]++
}

// Known is what was recorded, a value later records never change.
func (b *KnownBuilder) Known() Known {
	ids := slices.SortedFunc(maps.Keys(b.ids), compareBits)
	return Known{ids: ids, fill: maps.Clone(b.fill)}
}

// ID is an id, an item's or an element's.
type ID interface{ bits() [16]byte }

func (id ItemID) bits() [16]byte    { return id.b }
func (id ElementID) bits() [16]byte { return id.b }

// ShortID is an item as a person types it: its full id, or segments,
// with no prefix.
//
//sumtype:decl
type ShortID interface {
	shortID()
	String() string
}

// FullID is a whole id, which names its item wherever it sits.
type FullID struct{ ID ItemID }

// Segments is one or more segments joined by '.': the last the item's own,
// the rest its ancestors' nearest last, as rendered.
type Segments struct{ s string }

func (f FullID) String() string { return f.ID.String() }

func (FullID) shortID()   {}
func (Segments) shortID() {}

// List is the segments, the top-most first.
func (s Segments) List() []Segment {
	var out []Segment
	for _, seg := range strings.Split(s.s, ".") {
		out = append(out, Segment{s: seg})
	}
	return out
}

func (s Segments) String() string { return s.s }

// ParseShortID reads an item reference: bare, or under the active prefix
// as `<prefix>-…`; a full id or segments. Any other prefix is refused as
// another project's (§3.2).
func ParseShortID(s string, active Prefix) (ShortID, error) {
	body := s
	if pre, rest, ok := strings.Cut(s, "-"); ok {
		if _, err := ParsePrefix(pre); err != nil {
			return nil, fmt.Errorf("%q is not an item id: %w", s, err)
		}
		if pre != active.s {
			return nil, fmt.Errorf("%q is under the prefix %s, another project's; this one's is %s", s, pre, active)
		}
		body = rest
	}
	if id, err := ParseItemID(body); err == nil {
		return FullID{ID: id}, nil
	}
	for _, seg := range strings.Split(body, ".") {
		if _, err := ParseSegment(seg); err != nil {
			return nil, fmt.Errorf("%q is not an item id: %w", s, err)
		}
	}
	return Segments{s: body}, nil
}

// Render is an item's text under prefix: its segments, the top-most first,
// joined by '.'; an item with none renders by its full id.
func Render(prefix Prefix, id ItemID, segs []Segment) string {
	if len(segs) == 0 {
		return prefix.s + "-" + id.String()
	}
	parts := make([]string, len(segs))
	for i, s := range segs {
		parts[i] = s.s
	}
	return prefix.s + "-" + strings.Join(parts, ".")
}

// About is what an event is about: the workspace's root, an item, an
// element of an item, or, for a create, the item or element it creates
// with the width of the segment fixed at its mint. A root carries no
// element, and a segment is a width of its own id, never text that could
// disagree with it `[LAW:types-are-the-program]`.
//
//sumtype:decl
type About interface{ about() }

// Root is the workspace's root node.
type Root struct{}

// Item is one item.
type Item struct{ ID ItemID }

// Element is element ID of item Of.
type Element struct {
	Of ItemID
	ID ElementID
}

// NewItem is the item a create creates, its segment ID's first Width
// digits.
type NewItem struct {
	ID    ItemID
	Width int
}

// NewElement is the element of item Of a create creates, its segment ID's
// first Width digits.
type NewElement struct {
	Of    ItemID
	ID    ElementID
	Width int
}

func (Root) about()       {}
func (Item) about()       {}
func (Element) about()    {}
func (NewItem) about()    {}
func (NewElement) about() {}

// Segment is the segment the create fixed.
func (n NewItem) Segment() Segment { return n.ID.Segment(n.Width) }

// Segment is the segment the create fixed.
func (n NewElement) Segment() Segment { return n.ID.Segment(n.Width) }

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
	case NewItem:
		return Item{ID: a.ID}
	case Element:
		return Item{ID: a.Of}
	case NewElement:
		return Item{ID: a.Of}
	}
	panic(fmt.Sprintf("events: %T is not an About", a))
}
