package config

import (
	"fmt"
	"slices"
	"sort"
)

// Stage is what the engine does with a column: the first one a ticket starts
// in, a middle one, or one a ticket is done in. It is read off the column's
// variant, never stored beside it.
type Stage uint8

const (
	Initial Stage = iota + 1
	Active
	Terminal
)

func (s Stage) String() string {
	switch s {
	case Initial:
		return "initial"
	case Active:
		return "active"
	case Terminal:
		return "terminal"
	}
	return fmt.Sprintf("no stage (%d)", uint8(s))
}

// Column is one column of the graph. Its three variants are the only shapes
// a column has: a terminal column has no next and no exit list, so a
// workspace cannot write one.
type Column interface {
	isColumn()
	Name() string
	Stage() Stage
	Aliases() []string
	// Next names the columns a forward edge reaches. A terminal column has none.
	Next() []string
	// Entry is what a ticket needs before it arrives here.
	Entry() []Requirement
	// Exit is what a ticket needs before it leaves along a forward edge. A
	// terminal column has none.
	Exit() []Requirement
}

type columnBase struct {
	name    string
	aliases []string
	entry   []Requirement
}

// InitialColumn is where a ticket starts: one per graph.
type InitialColumn struct {
	columnBase
	next []string
	exit []Requirement
}

// ActiveColumn is a column a ticket passes through.
type ActiveColumn struct {
	columnBase
	next []string
	exit []Requirement
}

// TerminalColumn is a column a ticket is done in. At least one per graph.
type TerminalColumn struct {
	columnBase
}

func (InitialColumn) isColumn()  {}
func (ActiveColumn) isColumn()   {}
func (TerminalColumn) isColumn() {}

func (c columnBase) Name() string           { return c.name }
func (c columnBase) Aliases() []string      { return slices.Clone(c.aliases) }
func (c columnBase) Entry() []Requirement   { return slices.Clone(c.entry) }
func (c InitialColumn) Stage() Stage        { return Initial }
func (c ActiveColumn) Stage() Stage         { return Active }
func (c TerminalColumn) Stage() Stage       { return Terminal }
func (c InitialColumn) Next() []string      { return slices.Clone(c.next) }
func (c ActiveColumn) Next() []string       { return slices.Clone(c.next) }
func (TerminalColumn) Next() []string       { return nil }
func (c InitialColumn) Exit() []Requirement { return slices.Clone(c.exit) }
func (c ActiveColumn) Exit() []Requirement  { return slices.Clone(c.exit) }
func (TerminalColumn) Exit() []Requirement  { return nil }

// ColumnGraph is one column graph: its columns by name, its aliases, its
// one initial column, and for each column some next names, the columns
// whose next does. Only buildGraph makes one.
type ColumnGraph struct {
	initial string
	byName  map[string]Column
	aliases map[string]string
	entered map[string][]string
}

// Edge is how a move arrives: along a next the source lists, which the
// source's exit list guards; into a column no next names, which every
// column reaches; or into the column it is in, giving its fields again,
// which leaves nothing. Each carries the destination's entry list.
type Edge uint8

const (
	Forward Edge = iota + 1
	Universal
	Stay
)

// Reach is the edge from the column recorded as from to the declared
// column to, and false when there is none: a column some next names is
// entered only along those edges, and one no next names from every column
// (columns-and-phases.md). A from the graph does not declare lists no next,
// so the universal edges are its way out `[LAW:types-are-the-program]`.
func (g ColumnGraph) Reach(from, to string) (Edge, bool) {
	if from == to {
		return Stay, true
	}
	if c, ok := g.byName[from]; ok && slices.Contains(c.Next(), to) {
		return Forward, true
	}
	if _, named := g.entered[to]; !named {
		return Universal, true
	}
	return 0, false
}

// EnteredFrom is the columns whose next names to, sorted: the only columns
// a move into to may leave.
func (g ColumnGraph) EnteredFrom(to string) []string { return slices.Clone(g.entered[to]) }

// Resolve reads a recorded column name: a declared column, or an alias of
// one. A name neither declares nor aliases reports false, which is how a
// renamed-away name with no alias shows up as undeclared.
func (g ColumnGraph) Resolve(recorded string) (Column, bool) {
	if c, ok := g.byName[recorded]; ok {
		return c, true
	}
	if target, ok := g.aliases[recorded]; ok {
		return g.byName[target], true
	}
	return nil, false
}

// Initial is the column a ticket starts in.
func (g ColumnGraph) Initial() Column { return g.byName[g.initial] }

// Names is every declared column name, sorted.
func (g ColumnGraph) Names() []string {
	names := make([]string, 0, len(g.byName))
	for n := range g.byName {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// buildGraph is the one enforcer of a column graph's shape `[LAW:single-enforcer]`.
// cs are the columns the graph holds, and every next names one of them. keyOf
// names each column's key path for its refusals.
func buildGraph(cs map[string]Column, keyOf func(string) KeyPath) (ColumnGraph, []Refusal) {
	var refusals []Refusal
	g := ColumnGraph{byName: map[string]Column{}, aliases: map[string]string{}, entered: map[string][]string{}}
	var initials []string
	terminals := 0
	for _, name := range sortedKeys(cs) {
		c := cs[name]
		g.byName[name] = c
		if c.Stage() == Initial {
			initials = append(initials, name)
		}
		if c.Stage() == Terminal {
			terminals++
		}
	}
	switch len(initials) {
	case 0:
		refusals = append(refusals, Refusal{Key: "columns", Reason: ReasonNoInitial, Message: "the graph has no initial column: one column sets initial = true"})
	case 1:
		g.initial = initials[0]
	default:
		for _, name := range initials[1:] {
			refusals = append(refusals, Refusal{Key: keyOf(name), Reason: ReasonManyInitial, Message: fmt.Sprintf("%s is a second initial column: only %s may set initial = true", name, initials[0])})
		}
		g.initial = initials[0]
	}
	if terminals == 0 {
		refusals = append(refusals, Refusal{Key: "columns", Reason: ReasonNoTerminal, Message: "the graph has no terminal column: one column sets terminal = true"})
	}
	claimed := map[string]string{}
	for _, name := range sortedKeys(cs) {
		c := cs[name]
		for _, next := range c.Next() {
			if _, declared := cs[next]; !declared {
				refusals = append(refusals, Refusal{Key: keyed(keyOf(name), "next"), Reason: ReasonNextUndeclared, Message: fmt.Sprintf("next names %s, which no column declares", next)})
			}
			if slices.Contains(g.entered[next], name) {
				refusals = append(refusals, Refusal{Key: keyed(keyOf(name), "next"), Reason: ReasonDuplicateNext, Message: fmt.Sprintf("next names %s twice", next)})
				continue
			}
			g.entered[next] = append(g.entered[next], name)
		}
		for _, a := range c.Aliases() {
			if _, declared := cs[a]; declared {
				refusals = append(refusals, Refusal{Key: keyed(keyOf(name), "aliases"), Reason: ReasonAliasCollides, Message: fmt.Sprintf("alias %s equals the declared column %s", a, a)})
				continue
			}
			if other, dup := claimed[a]; dup {
				refusals = append(refusals, Refusal{Key: keyed(keyOf(name), "aliases"), Reason: ReasonDuplicateAlias, Message: fmt.Sprintf("alias %s is claimed by %s and %s", a, other, name)})
				continue
			}
			claimed[a] = name
			g.aliases[a] = name
		}
	}
	return g, refusals
}

func sortedKeys[K ~string, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
