package evaluate

import (
	"fmt"

	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

// AncestryOf is the sections a thing placed in c is read through, nearest
// first: the one mapping from where a thing sits to its configuration
// `[LAW:one-source-of-truth]`. An item is read through itself and its
// ancestors; a home through its section when its level declares one; no
// container through nothing but the root. Structure is the fold's: each
// step is State.Container, so a forged parent link is no step.
func AncestryOf(s fold.State, schema config.Schema, c fold.Container) config.Ancestry {
	var a config.Ancestry
	for c != nil {
		switch at := c.(type) {
		case fold.Under:
			a = append(a, config.ItemNode(at.Parent))
			c = s.Container(at.Parent)
		case fold.Home:
			if lv, ok := schema.Level(at.Holds.Int()); ok && lv.Home != "" {
				a = append(a, config.HomeNode(lv.Home))
			}
			c = nil
		default:
			panic(fmt.Sprintf("evaluate: %T is not a Container", c))
		}
	}
	return a
}

// ItemAncestry is an existing item's ancestry: the item, then what it sits
// in.
func ItemAncestry(s fold.State, schema config.Schema, id events.ItemID) config.Ancestry {
	return append(config.Ancestry{config.ItemNode(id)}, AncestryOf(s, schema, s.Container(id))...)
}

// StatusIn is the column facts are in under eff: the settled column read
// through the graph, so a column renamed with an alias reads by its new
// name; a name the graph no longer reads stays as recorded; and with no
// transition, the zero Facts of a new item included, the graph's initial
// column.
func StatusIn(eff config.Effective, facts fold.Facts) string {
	switch f := facts.Plain(fold.Status).(type) {
	case fold.Unset:
		return eff.Columns.Initial().Name()
	case fold.Settled:
		if c, ok := eff.Columns.Resolve(f.Value); ok {
			return c.Name()
		}
		return f.Value
	}
	panic(fmt.Sprintf("evaluate: %T is not a PlainField", facts.Plain(fold.Status)))
}
