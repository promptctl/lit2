package evaluate

import (
	"fmt"
	"slices"
	"strings"

	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

// CheckApply is the check (specifications/columns-and-requirements.md,
// "Validity of a column graph") a version passes before it is applied:
// every faulty column graph of next on an ancestry the state reaches, and
// every column next drops that a ticket is in, under some version in force
// now, with no alias saying where it goes. live is every live version, so a
// resolution cannot strand a ticket moved into a column only the version it
// replaces declared. Faults come keyed and in a stable order; none is
// overridable (specifications/workspace-file.md, "Validation"). It is pure: the state and the versions are values.
func CheckApply(st fold.State, live []config.Schema, next config.Schema) []config.Refusal {
	return append(unresolved(st, next), stranded(st, live, next)...)
}

// unresolved is every refusal next's Effective gives on an ancestry the
// state reaches: the root's, each home section's, and each created item's,
// a section layered over its ancestors as every read layers it. An item
// section is judged on its item's ancestry, which an item this store has
// not seen does not have here. Parse checks the root graph
// alone; a stacked graph needs an ancestry, so it is checked here. Each
// fault once.
func unresolved(st fold.State, next config.Schema) []config.Refusal {
	ancestries := []config.Ancestry{nil}
	for key := range next.Sections() {
		if _, isItem := key.Item(); !isItem {
			ancestries = append(ancestries, config.Ancestry{key})
		}
	}
	for _, it := range st.Items() {
		ancestries = append(ancestries, ItemAncestry(st, next, it.ID))
	}
	var out []config.Refusal
	for _, a := range ancestries {
		if _, err := Effective(next, a); err != nil {
			for _, r := range err.(*config.ResolutionRefused).Refusals {
				if !slices.Contains(out, r) {
					out = append(out, r)
				}
			}
		}
	}
	return sorted(out)
}

// stranded is one refusal per column a live version declares and next
// reads nowhere for a ticket in it: the key path of the old declaration,
// the tickets, and the aliases line that would say where they go. A
// recorded name no live version reads is S-6's undeclared column already,
// and blocks nothing; nor does a ticket rm removed, or one under a node it
// removed, which is no work.
func stranded(st fold.State, live []config.Schema, next config.Schema) []config.Refusal {
	type held struct {
		key, scope config.KeyPath
		name       string
		tickets    []string
	}
	var found []*held
	for _, it := range st.Items() {
		l, ok := it.Level.(fold.Leveled)
		recorded, settled := it.Facts.Plain(fold.Status).(fold.Settled)
		if !ok || !l.Leaf() || !settled || gone(st, it.ID) {
			continue
		}
		if eff, err := Effective(next, ItemAncestry(st, next, it.ID)); err != nil || resolves(eff, recorded.Value) {
			// a graph next refuses is unresolved's fault, named there
			continue
		}
		key, scope, ok := declaredIn(st, live, it.ID, recorded.Value)
		if !ok {
			continue
		}
		i := slices.IndexFunc(found, func(h *held) bool { return h.key == key && h.name == recorded.Value })
		if i < 0 {
			found = append(found, &held{key: key, scope: scope, name: recorded.Value})
			i = len(found) - 1
		}
		found[i].tickets = append(found[i].tickets, events.Render(next.Prefix(), it.ID, st.Path(it.ID)))
	}
	out := make([]config.Refusal, 0, len(found))
	for _, h := range found {
		out = append(out, config.Refusal{Key: h.key, Reason: config.ReasonColumnHeld, Message: fmt.Sprintf(
			"%s holds %s, and this version declares no column it reads as; say where they go: add `%s.aliases = [%q]` to the column that takes them",
			h.name, strings.Join(h.tickets, ", "), h.scope, h.name)})
	}
	return sorted(out)
}

// declaredIn is where the first live version that reads recorded for id
// declares it: the column's key path, and the scope's placeholder column a
// person names the new home in.
func declaredIn(st fold.State, live []config.Schema, id events.ItemID, recorded string) (key, scope config.KeyPath, ok bool) {
	for _, s := range live {
		eff, err := Effective(s, ItemAncestry(st, s, id))
		if err != nil {
			continue
		}
		if c, ok := eff.Columns.Resolve(recorded); ok {
			origin := eff.ColumnOrigin[c.Name()]
			return config.ColumnKey(origin, c.Name()), config.ColumnKey(origin, "<column>"), true
		}
	}
	return "", "", false
}

func resolves(eff config.Effective, name string) bool {
	_, ok := eff.Columns.Resolve(name)
	return ok
}

func sorted(rs []config.Refusal) []config.Refusal {
	slices.SortFunc(rs, func(a, b config.Refusal) int {
		return strings.Compare(string(a.Key)+"\x00"+a.Message, string(b.Key)+"\x00"+b.Message)
	})
	return rs
}
