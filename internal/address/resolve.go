package address

import (
	"fmt"
	"slices"
	"strings"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

// NotFound is a short id that names no item.
type NotFound struct{ ID events.ShortID }

// Ambiguous is a short id that names two or more items, each listed: two
// writers offline can fix one segment for two items, and after sync the
// segment names neither (§3.2).
type Ambiguous struct {
	ID         events.ShortID
	Candidates []events.ItemID
}

func (n NotFound) Error() string { return fmt.Sprintf("%s names no item", n.ID) }

func (a Ambiguous) Error() string {
	names := make([]string, len(a.Candidates))
	for i, c := range a.Candidates {
		names[i] = c.String()
	}
	return fmt.Sprintf("%s names %d items: %s", a.ID, len(a.Candidates), strings.Join(names, ", "))
}

// Resolve is the one created item id names in st. A full id names its
// item wherever it sits; segments name the created items whose rendered path
// ends with them, so the last is the item's own segment, unique among every
// id its writer had folded, and any before it are its ancestors' nearest
// last. Hierarchy narrows a match and never makes one (§3.2).
func Resolve(st fold.State, id events.ShortID) (events.ItemID, error) {
	switch id := id.(type) {
	case events.FullID:
		if f, ok := st.Item(id.ID).(fold.Found); ok && created(f.Item) {
			return id.ID, nil
		}
		return events.ItemID{}, NotFound{ID: id}
	case events.Segments:
		want := id.List()
		var matches []events.ItemID
		for _, it := range st.Items() {
			path := st.Path(it.ID)
			if len(path) >= len(want) && slices.Equal(path[len(path)-len(want):], want) {
				matches = append(matches, it.ID)
			}
		}
		switch len(matches) {
		case 0:
			return events.ItemID{}, NotFound{ID: id}
		case 1:
			return matches[0], nil
		}
		slices.SortFunc(matches, events.ItemID.Compare)
		return events.ItemID{}, Ambiguous{ID: id, Candidates: matches}
	}
	panic(fmt.Sprintf("address: %T is not a ShortID", id))
}

// created is whether it was created: an id that facts only name is no item.
func created(it fold.Item) bool {
	switch it.Origin.(type) {
	case fold.Created, fold.Collided:
		return true
	case fold.Uncreated:
	}
	return false
}
