package fold

import (
	"fmt"
	"time"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/events/wire"
	"github.com/promptctl/lit2/internal/git"
)

// Rule is one kind's fold rule: the changes its event makes. It sees its
// event and never the state, so it merges in any order
// `[LAW:effects-at-boundaries]`.
type Rule[P events.Payload] func(Fact[P]) Applies

// Fact is one event as its rule sees it: its payload, its stamp, and the
// node it is about, narrowed to what the kind accepts.
type Fact[P events.Payload] struct {
	Payload P
	Stamp   Stamp
	At      Target
}

// Applies is what a rule makes of its event.
//
//sumtype:decl
type Applies interface{ applies() }

// Changes is the writes an event makes, each one a shape's.
type Changes struct{ List []Change }

// Misaddressed is an event about a node its kind does not write: Want says
// what it should have been about.
type Misaddressed struct{ Want string }

func (Changes) applies()      {}
func (Misaddressed) applies() {}

// Target is what an event is about, narrowed to what its kind accepts: a
// node that exists, or a node its create makes.
//
//sumtype:decl
type Target interface{ target() }

// Node is a node a change writes: an item or an element of one. A create
// subject is no Node, so no write can target one `[LAW:types-are-the-program]`.
//
//sumtype:decl
type Node interface {
	Target
	node()
}

// OnItem is an item.
type OnItem struct{ ID events.ItemID }

// OnElement is element ID of item Of.
type OnElement struct {
	Of events.ItemID
	ID events.ElementID
}

// OnNewItem is the item a create makes.
type OnNewItem struct{ New events.NewItem }

// OnNewElement is the element a create makes.
type OnNewElement struct{ New events.NewElement }

// OnRoot is the root, the workspace an apply changes the model of. It is
// no Node, so no field write can target it `[LAW:types-are-the-program]`.
type OnRoot struct{}

func (OnItem) target()       {}
func (OnElement) target()    {}
func (OnNewItem) target()    {}
func (OnNewElement) target() {}
func (OnRoot) target()       {}
func (OnItem) node()         {}
func (OnElement) node()      {}

// Aim is the About shapes a kind accepts.
type Aim uint8

// The aims.
const (
	// Items is an item alone.
	Items Aim = iota + 1
	// Elements is an element of an item alone.
	Elements
	// Nodes is an item or an element.
	Nodes
	// NewItems is the item a create makes.
	NewItems
	// NewElements is the element a create makes.
	NewElements
	// Root is the root alone.
	Root
)

// target narrows about to the node a kind of this aim writes; false is an
// About the kind does not accept `[LAW:parse-dont-validate]`.
func (a Aim) target(about events.About) (Target, bool) {
	switch at := about.(type) {
	case events.Item:
		return OnItem{ID: at.ID}, a == Items || a == Nodes
	case events.Element:
		return OnElement{Of: at.Of, ID: at.ID}, a == Elements || a == Nodes
	case events.NewItem:
		return OnNewItem{New: at}, a == NewItems
	case events.NewElement:
		return OnNewElement{New: at}, a == NewElements
	case events.Root:
		return OnRoot{}, a == Root
	}
	panic(fmt.Sprintf("fold: %T is not an About", about))
}

// misaddressed is what an event about a node this aim does not accept is.
func (a Aim) misaddressed() Misaddressed {
	switch a {
	case Items:
		return Misaddressed{Want: "an item"}
	case Elements:
		return Misaddressed{Want: "an element of an item"}
	case Nodes:
		return Misaddressed{Want: "an item or an element of one"}
	case NewItems:
		return Misaddressed{Want: "the item it creates"}
	case NewElements:
		return Misaddressed{Want: "the element it creates"}
	case Root:
		return Misaddressed{Want: "the root"}
	}
	panic(fmt.Sprintf("fold: %d is not an Aim", uint8(a)))
}

// Kind is one kind of event: its decoders and its rule. Only On builds
// one, so the row and the rule name one payload type.
type Kind struct {
	row   events.KindRow
	aim   Aim
	apply func(Stamp, Target, events.Payload) Applies
}

// On is the kind of P: the About shapes it accepts, its rule, and its
// decoders, decode[i] reading version i+1. Adding a kind is a payload, its
// decoder and one On line; nothing else changes `[LAW:composability]`.
func On[P events.Payload](aim Aim, r Rule[P], decode ...func(*wire.Reader) (events.Versioned, error)) Kind {
	return Kind{row: events.Row[P](decode...), aim: aim, apply: func(st Stamp, at Target, p events.Payload) Applies {
		payload, ok := p.(P)
		if !ok {
			// [LAW:no-silent-failure] the row's decoder returned another type
			// than its rule reads: a build fault in the kind
			panic(fmt.Sprintf("fold: %s decodes to %T, which its rule does not read", p.Kind(), p))
		}
		return r(Fact[P]{Payload: payload, Stamp: st, At: at})
	}}
}

// Registry is every kind this binary folds: one table for decoding and
// folding `[LAW:one-source-of-truth]`. It is built once at the root and
// never changed.
type Registry struct {
	kinds  map[events.Kind]Kind
	decode events.Kinds
}

// NewRegistry is the registry of ks; two of one kind is a build fault.
func NewRegistry(ks ...Kind) Registry {
	by := map[events.Kind]Kind{}
	rows := make([]events.KindRow, 0, len(ks))
	for _, k := range ks {
		if _, ok := by[k.row.Kind()]; ok {
			panic(fmt.Sprintf("fold: two kinds %s", k.row.Kind()))
		}
		by[k.row.Kind()] = k
		rows = append(rows, k.row)
	}
	return Registry{kinds: by, decode: events.NewKinds(rows...)}
}

// Kinds is the decode table events.Decode reads records with.
func (r Registry) Kinds() events.Kinds { return r.decode }

// Change is one write of one shape: the closed set every rule returns.
//
//sumtype:decl
type Change interface{ change() }

// CreateItem creates New, placed as its create says.
type CreateItem struct {
	New   events.NewItem
	Place events.Placement
}

// CreateElement creates New, shown in its item's list In.
type CreateElement struct {
	New events.NewElement
	In  events.Name
}

// Write writes Value to field Field of At, replacing the writes Over.
type Write struct {
	At    Node
	Field events.Name
	Value events.Value
	Over  []events.EventID
}

// Add adds Member to set Set of At.
type Add struct {
	At     Node
	Set    events.Name
	Member events.Name
}

// Remove removes the adds Observed of Member from set Set of At.
type Remove struct {
	At       Node
	Set      events.Name
	Member   events.Name
	Observed []events.EventID
}

// Apply makes the version Bytes, whose git id is Blob, the workspace's,
// replacing the applies Over.
type Apply struct {
	Blob  git.ObjectID
	Bytes []byte
	Over  []events.EventID
}

func (CreateItem) change()    {}
func (CreateElement) change() {}
func (Write) change()         {}
func (Add) change()           {}
func (Remove) change()        {}
func (Apply) change()         {}

// Stamp is which event made a fact, where it sorts, who wrote it and the
// writer's clock when it did: data, never order (INV:wall-time-is-data).
type Stamp struct {
	Event      events.EventID
	Order      events.OrderKey
	By         events.Attribution
	RecordedAt time.Time
}

func stampOf(e events.Event) Stamp {
	h := e.Header()
	return Stamp{Event: e.ID(), Order: e.Order(), By: h.Attribution(), RecordedAt: h.RecordedAt()}
}
