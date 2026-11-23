package config

import (
	"time"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/field"
)

// Schema is one workspace version as this binary reads it: the one typed value
// the enforcer, evaluate and the field table judge state under
// (specifications/workspace-file.md, "Validation"). Only Parse builds one, so every field is unexported and a
// consumer receives a Loaded, never a Schema it made itself
// `[LAW:types-are-the-program]` (an exception: Go admits a zero Schema; a
// zero Schema holds no levels and no root columns, and no caller reads one).
type Schema struct {
	prefix   events.Prefix
	gate     ReaderGate
	labels   field.Enum
	levels   []Level
	table    Table
	links    map[string]LinkType
	checks   map[string]Check
	root     RootFrame
	sections map[NodeKey]Section
}

// Level is one declared level. Index 0 is level 1, the leaf, which has no
// home; every level above it names one. The fields it declares are rows of
// the table, at its items. Template is the values a new item of the level
// starts with, each canonical in its field's kind.
type Level struct {
	Name     string
	Home     string
	Template map[field.Path][]string
}

// LinkType is a link name's kind and the level pairs it may join.
type LinkType struct {
	Kind    LinkKind
	Between [][2]int
}

// LinkKind is one of the two kinds the engine knows.
type LinkKind uint8

const (
	Dependency LinkKind = iota + 1
	Relates
)

// Scope is which items a test reads: the ticket itself, or the ticket and
// its ancestors.
type Scope uint8

const (
	Self Scope = iota + 1
	Ancestors
)

// Severity is what an unmet requirement does: refuse the move, or print and
// let it proceed. There is no default.
type Severity uint8

const (
	Error Severity = iota + 1
	Warning
)

// Test is one atom and the scope it reads. DeclaredIn names the check that
// writes it, so a failure prints under that check's name; empty means a
// column wrote it directly.
type Test struct {
	Atom       field.Atom
	Scope      Scope
	DeclaredIn string
}

// Check is a named list of tests, flattened: a check that names another
// check holds that check's tests too, each keeping its own declaring name.
type Check struct {
	Name  string
	Tests []Test
}

// Requirement is one entry of a column's entry or exit list: a test with the
// severity its entry set.
type Requirement struct {
	Test     Test
	Severity Severity
}

// RootFrame is what the root declares for every item: its columns and the
// claim lifetime every section inherits unless it says otherwise.
type RootFrame struct {
	Columns  ColumnGraph
	Declared map[string]Column
	ClaimTTL time.Duration
}

// Section is a node's own declarations: columns it adds or overrides, and a
// claim lifetime if it sets one.
type Section struct {
	Columns  map[string]Column
	ClaimTTL Declared[time.Duration]
}

// Declared is a value that a section either declares or leaves to its
// ancestors. The zero value declares nothing.
type Declared[T any] struct {
	v  T
	ok bool
}

// Get is the declared value and whether it was declared.
func (d Declared[T]) Get() (T, bool) { return d.v, d.ok }

// NodeKey names the node a section belongs to: an item by its full id, or a
// declared home by its name. Exactly one of the two is set.
type NodeKey struct {
	item events.ItemID
	home string
}

// ItemNode is the section key of one item.
func ItemNode(id events.ItemID) NodeKey { return NodeKey{item: id} }

// HomeNode is the section key of a declared home.
func HomeNode(home string) NodeKey { return NodeKey{home: home} }

// Item is the item the key names, and false for a home.
func (k NodeKey) Item() (events.ItemID, bool) { return k.item, k.home == "" }

// Ancestry is the nodes an item's effective configuration is read through,
// nearest first, starting at the item itself. The root is implicit.
type Ancestry []NodeKey

// Origin is where an effective value came from: the root, or one section.
type Origin interface{ isOrigin() }

// RootOrigin is a value the root declared.
type RootOrigin struct{}

// SectionOrigin is a value a section declared.
type SectionOrigin struct{ Key NodeKey }

func (RootOrigin) isOrigin()    {}
func (SectionOrigin) isOrigin() {}

// Effective is the configuration an item is judged under: its column graph
// and claim lifetime after every section on its ancestry has layered over the
// root, and where each value came from.
type Effective struct {
	Columns      ColumnGraph
	ClaimTTL     time.Duration
	ColumnOrigin map[string]Origin
	TTLOrigin    Origin
}

// ResolutionRefused is an ancestry whose layered columns do not form a graph.
// It names the faults with the key path of the section that declared each.
type ResolutionRefused struct {
	Ancestry Ancestry
	Refusals []Refusal
}

func (e *ResolutionRefused) Error() string {
	return (&Refused{Source: "the configuration of this ancestry", Refusals: e.Refusals}).Error()
}

// Level reads level n, counting from 1 at the leaf, and whether it is declared.
func (s Schema) Level(n int) (Level, bool) {
	if n < 1 || n > len(s.levels) {
		return Level{}, false
	}
	return s.levels[n-1], true
}

// Levels is how many levels the version declares.
func (s Schema) Levels() int { return len(s.levels) }

// Table is the version's field table.
func (s Schema) Table() Table { return s.table }

// Prefix is the project prefix.
func (s Schema) Prefix() events.Prefix { return s.prefix }

// Gate is the oldest reader the version allows.
func (s Schema) Gate() ReaderGate { return s.gate }

// Labels is the declared label set.
func (s Schema) Labels() field.Enum { return s.labels }

// Links reads the link types by name.
func (s Schema) Links() map[string]LinkType { return s.links }

// Checks reads the checks by name, flattened.
func (s Schema) Checks() map[string]Check { return s.checks }

// Root reads the root frame.
func (s Schema) Root() RootFrame { return s.root }

// Sections reads the node sections by key.
func (s Schema) Sections() map[NodeKey]Section { return s.sections }

// Effective resolves the configuration an ancestry judges an item under. The
// root is the base; each section on the ancestry layers over it, the nearest
// winning per column name and per claim lifetime. The layered graph is then
// checked as a whole by the one graph enforcer, so a section that names a
// column no section on the ancestry declares is refused here
// `[LAW:single-enforcer]`.
func (s Schema) Effective(a Ancestry) (Effective, error) {
	merged := map[string]Column{}
	origin := map[string]Origin{}
	for name, c := range s.root.Declared {
		merged[name] = c
		origin[name] = RootOrigin{}
	}
	ttl, ttlOrigin := s.root.ClaimTTL, Origin(RootOrigin{})
	// Farthest first, so a nearer section overwrites what a farther one set.
	for i := len(a) - 1; i >= 0; i-- {
		sec, ok := s.sections[a[i]]
		if !ok {
			continue
		}
		for name, c := range sec.Columns {
			merged[name] = c
			origin[name] = SectionOrigin{Key: a[i]}
		}
		if v, ok := sec.ClaimTTL.Get(); ok {
			ttl, ttlOrigin = v, SectionOrigin{Key: a[i]}
		}
	}
	g, refusals := buildGraph(merged, func(name string) KeyPath { return ColumnKey(origin[name], name) })
	if len(refusals) > 0 {
		return Effective{}, &ResolutionRefused{Ancestry: a, Refusals: refusals}
	}
	return Effective{Columns: g, ClaimTTL: ttl, ColumnOrigin: origin, TTLOrigin: ttlOrigin}, nil
}

// ColumnKey is the key path that declares column name where o is: the
// root's `columns.<name>`, or a section's `nodes.<key>.columns.<name>`.
func ColumnKey(o Origin, name string) KeyPath {
	switch o := o.(type) {
	case SectionOrigin:
		return keyed(keyed("nodes", nodeSegment(o.Key)), "columns."+name)
	case RootOrigin:
	}
	return "columns." + KeyPath(name)
}
