package evaluate

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/field"
	"github.com/promptctl/lit2/internal/fold"
)

// Column is where a leaf's status puts it under its graph: the column the
// graph declares, or a recorded name the graph no longer reads, which has
// neither flag and so is active.
type Column struct {
	Name     string
	Stage    config.Stage
	Declared bool
}

// ColumnIn is the column facts are in under eff, read through StatusIn.
func ColumnIn(eff config.Effective, facts fold.Facts) Column {
	name := StatusIn(eff, facts)
	if c, ok := eff.Columns.Resolve(name); ok {
		return Column{Name: name, Stage: c.Stage(), Declared: true}
	}
	return Column{Name: name, Stage: config.Active}
}

// Removed is whether facts hold the deleted flag rm sets: the one reading
// of the tombstone `[LAW:one-source-of-truth]`.
func Removed(f fold.Facts) bool {
	s, ok := f.Plain(events.MustName(string(field.Deleted))).(fold.Settled)
	return ok && s.Value == "true"
}

// Stage is an item's stage, derived on every read and never stored: a
// leaf's is its column's; a node's is terminal when every child's is,
// initial when every child's is or it has none, and active otherwise
// (specifications/columns-and-requirements.md, "The graph and stage"). A
// removed child is no work, so it counts for
// nothing. A graph the configuration refuses is the refusal a status read
// gets.
func (v View) Stage(id events.ItemID) (config.Stage, error) {
	if v.Leaf(id) {
		c, err := v.Column(id)
		return c.Stage, err
	}
	initial, terminal := true, true
	for _, child := range v.live(id) {
		s, err := v.Stage(child)
		if err != nil {
			return 0, err
		}
		initial = initial && s == config.Initial
		terminal = terminal && s == config.Terminal
	}
	switch {
	case initial:
		return config.Initial, nil
	case terminal:
		return config.Terminal, nil
	}
	return config.Active, nil
}

// Count is how many leaves under a node are in one column.
type Count struct {
	Column Column
	N      int
}

// Rollup is the count of the leaves under node id per column, each read
// under its own graph, removed ones counting for nothing: by stage, then
// by name.
func (v View) Rollup(id events.ItemID) ([]Count, error) {
	counts := map[Column]int{}
	var walk func(events.ItemID) error
	walk = func(n events.ItemID) error {
		for _, child := range v.live(n) {
			if !v.Leaf(child) {
				if err := walk(child); err != nil {
					return err
				}
				continue
			}
			c, err := v.Column(child)
			if err != nil {
				return err
			}
			counts[c]++
		}
		return nil
	}
	if err := walk(id); err != nil {
		return nil, err
	}
	out := make([]Count, 0, len(counts))
	for c, n := range counts {
		out = append(out, Count{Column: c, N: n})
	}
	slices.SortFunc(out, func(a, b Count) int {
		return cmp.Or(cmp.Compare(a.Column.Stage, b.Column.Stage), cmp.Compare(a.Column.Name, b.Column.Name))
	})
	return out, nil
}

// Column is leaf id's column under the graph it is judged under.
func (v View) Column(id events.ItemID) (Column, error) {
	eff, err := Effective(v.schema, ItemAncestry(v.state, v.schema, id))
	if err != nil {
		return Column{}, err
	}
	return ColumnIn(eff, v.item(id).Facts), nil
}

// Leaf is whether item id is a leaf, as fold.Leveled reads it.
func (v View) Leaf(id events.ItemID) bool {
	l, ok := v.item(id).Level.(fold.Leveled)
	if !ok {
		panic(fmt.Sprintf("evaluate: %s is placed at no level", id))
	}
	return l.Leaf()
}

// live is the children of id rm has not removed.
func (v View) live(id events.ItemID) []events.ItemID {
	var out []events.ItemID
	for _, c := range v.state.Children(fold.Under{Parent: id}) {
		if !Removed(v.item(c).Facts) {
			out = append(out, c)
		}
	}
	return out
}

// item is the folded item id, which exists.
func (v View) item(id events.ItemID) fold.Item {
	return v.state.Item(id).(fold.Found).Item
}
