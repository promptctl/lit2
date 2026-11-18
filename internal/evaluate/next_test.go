package evaluate

import (
	"testing"
	"time"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

var now = time.Date(2026, time.November, 17, 12, 0, 0, 0, time.UTC)

// leafIn files a parentless leaf, as a writer that has folded s, and
// writes each field given on it, in order: status by a transition, the one
// write of a status, and any other by a field.set.
func leafIn(t *testing.T, s fold.State, n byte, fields ...[2]string) (fold.State, events.ItemID) {
	t.Helper()
	s, id := mint(t, s, s.Known(), create(events.AtLevel{Level: must(events.ParseLevel(1))}), [16]byte{n})
	for i, fv := range fields {
		f, v, k := fv[0], fv[1], byte(i+1)
		var p events.Payload = events.FieldSet{Field: events.MustName(f), Value: events.Plain{Canonical: v}}
		if f == "status" {
			p = events.Transition{From: events.MustName("backlog"), To: events.MustName(v)}
		}
		s, _ = mint(t, s, s.Known(), events.Act{About: events.Item{ID: id}, Payload: p}, [16]byte{n, k})
	}
	return s, id
}

func TestAnEmptyBacklogServesNothingAndConsideredNone(t *testing.T) {
	got, err := Of(fold.Replay(env, fold.NoFounding{}, nil).State, schema, now).Next()
	if err != nil || got.Head != (NoHead{}) || got.Considered != 0 || len(got.Passed) != 0 {
		t.Errorf("an empty backlog's next is %+v, %v; want no head, 0 considered, none passed", got, err)
	}
}

// TestNextServesTheFirstLaneWithAHeadAndNamesWhatItPassed: a finished lane
// and a removed leaf have no head, a head in an undeclared column is
// passed over by name, and the walk stops at the first head it serves.
func TestNextServesTheFirstLaneWithAHeadAndNamesWhatItPassed(t *testing.T) {
	s := founded
	s, _ = leafIn(t, s, 1, [2]string{"status", "done"})
	s, _ = leafIn(t, s, 2, [2]string{"deleted", "true"})
	s, stray := leafIn(t, s, 3, [2]string{"status", "limbo"})
	s, served := leafIn(t, s, 4)
	s, _ = leafIn(t, s, 5)
	got, err := Of(s, schema, now).Next()
	if err != nil {
		t.Fatal(err)
	}
	if got.Head != (Served{ID: served}) || got.Considered != 4 {
		t.Errorf("next is %+v; want %s served after 4 lanes considered", got, served)
	}
	if len(got.Passed) != 1 || got.Passed[0] != (Passed{Head: stray, Why: Undeclared{Column: "limbo"}}) {
		t.Errorf("next passed %+v; want only %s, its column limbo undeclared", got.Passed, stray)
	}
}

// TestALeafUnderARemovedNodeIsNoHead: rm on a feature removes the work
// below it, so next serves the next lane, not the feature's live leaf.
func TestALeafUnderARemovedNodeIsNoHead(t *testing.T) {
	s := founded
	s, feature := mint(t, s, s.Known(), create(events.AtLevel{Level: must(events.ParseLevel(2))}), [16]byte{1})
	s, _ = mint(t, s, s.Known(), create(events.BelowParent{Parent: feature}), [16]byte{2})
	s, _ = mint(t, s, s.Known(), events.Act{About: events.Item{ID: feature}, Payload: events.FieldSet{Field: events.MustName("deleted"), Value: events.Plain{Canonical: "true"}}}, [16]byte{1, 1})
	s, served := leafIn(t, s, 3)
	got, err := Of(s, schema, now).Next()
	if err != nil || got.Head != (Served{ID: served}) || got.Considered != 2 {
		t.Errorf("next is %+v, %v; want %s served after 2 lanes considered", got, err, served)
	}
}
