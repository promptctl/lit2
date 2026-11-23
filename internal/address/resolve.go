package address

import (
	"fmt"
	"slices"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

// candidate is one node a short id may name: its id, its full id's text,
// and its rendered path, read only when segments are matched.
type candidate[I comparable] struct {
	id   I
	full string
	path func() []events.Segment
}

// resolve is the one rule a short id names a node by, an item or an
// element alike `[LAW:single-enforcer]`. A full id names its node wherever
// it sits; segments name the nodes whose rendered path ends with them, so
// the last is the node's own segment, unique among every id its writer had
// folded, and any before it its ancestors', nearest last. Hierarchy
// narrows a match and never makes one (specifications/events-and-store.md,
// "Ids"). Two writers offline can fix
// one segment for two nodes, and then it names neither, listing both.
func resolve[I comparable](key events.ShortID, cands []candidate[I]) (I, error) {
	var none I
	var matches []candidate[I]
	switch k := key.(type) {
	case events.FullID:
		for _, c := range cands {
			if c.full == k.String() {
				return c.id, nil
			}
		}
	case events.Segments:
		want := k.List()
		for _, c := range cands {
			if p := c.path(); len(p) >= len(want) && slices.Equal(p[len(p)-len(want):], want) {
				matches = append(matches, c)
			}
		}
	default:
		panic(fmt.Sprintf("address: %T is not a ShortID", key))
	}
	switch len(matches) {
	case 0:
		return none, NotFound{ID: key.String()}
	case 1:
		return matches[0].id, nil
	}
	amb := Ambiguous{ID: key.String()}
	for _, m := range matches {
		amb.Candidates = append(amb.Candidates, m.full)
	}
	return none, amb
}

// Resolve is the one created item id names in st. An id that facts only
// name is no item, and Items lists none, so it is no candidate.
func Resolve(st fold.State, id events.ShortID) (events.ItemID, error) {
	var cands []candidate[events.ItemID]
	for _, it := range st.Items() { // by id, so the candidates are listed in that order
		cands = append(cands, candidate[events.ItemID]{id: it.ID, full: it.ID.String(), path: func() []events.Segment { return st.Path(it.ID) }})
	}
	return resolve(id, cands)
}

// resolveElement is the one element of it, in the list n, that key names,
// and its segment: an element's path is its own segment.
func resolveElement(it fold.Item, n events.Name, key events.ShortID) (events.ElementID, events.Segment, error) {
	type named struct {
		id  events.ElementID
		seg events.Segment
	}
	var cands []candidate[named]
	for _, e := range it.Elements {
		c, ok := fold.CreateOf(e.Origin)
		if !ok || e.In != n {
			continue
		}
		cands = append(cands, candidate[named]{id: named{e.ID, c.Segment}, full: e.ID.String(), path: func() []events.Segment { return []events.Segment{c.Segment} }})
	}
	k, err := resolve(key, cands)
	return k.id, k.seg, err
}
