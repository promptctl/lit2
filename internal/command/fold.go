package command

import (
	"fmt"

	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/store"
)

// The fold block as the line spells it, one object per fold that ran:
// where it started and why, what it read, checked and applied, and the
// state it left, every count present and zero when zero.
type (
	foldLine struct {
		Base       string         `json:"base"`
		Ref        string         `json:"ref,omitempty"`
		Why        string         `json:"why,omitempty"`
		Records    int            `json:"records"`
		Torn       int            `json:"torn"`
		Duplicates int            `json:"duplicates"`
		Late       int            `json:"late"`
		Decoded    decodedLine    `json:"decoded"`
		Checked    int            `json:"checked"`
		Trusted    int            `json:"trusted"`
		Exceptions int            `json:"exceptions"`
		Verdicts   verdictsLine   `json:"verdicts"`
		Applied    int            `json:"applied"`
		Carried    map[string]int `json:"carried"`
		Unapplied  map[string]int `json:"unapplied"`
		Counts     stateLine      `json:"counts"`
	}
	decodedLine struct {
		Readable    int            `json:"readable"`
		Unread      map[string]int `json:"unread"`
		Erased      int            `json:"erased"`
		Uncommitted int            `json:"uncommitted"`
		Malformed   int            `json:"malformed"`
	}
	verdictsLine struct {
		Valid        int `json:"valid"`
		Invalid      int `json:"invalid"`
		UnknownSuite int `json:"unknown_suite"`
	}
	stateLine struct {
		Items          int `json:"items"`
		Tombstoned     int `json:"tombstoned"`
		Comments       int `json:"comments"`
		Labels         int `json:"labels"`
		ProseConflicts int `json:"prose_conflicts"`
		Overruled      int `json:"overruled"`
		MixedForms     int `json:"mixed_forms"`
		Collisions     int `json:"collisions"`
		Uncreated      int `json:"uncreated"`
	}
)

func foldBlock(fs []fold.FoldFacts) []foldLine {
	var out []foldLine
	for _, f := range fs {
		l := foldLine{
			Records: f.Records, Torn: f.Torn, Duplicates: f.Duplicates, Late: f.Late,
			Decoded: decodedLine{Readable: f.Decoded.Readable, Unread: f.Decoded.Unread, Erased: f.Decoded.Erased, Uncommitted: f.Decoded.Uncommitted, Malformed: f.Decoded.Malformed},
			Checked: f.Checked, Trusted: f.Trusted, Exceptions: f.Exceptions,
			Verdicts: verdictsLine{Valid: f.Verdicts.Valid, Invalid: f.Verdicts.Invalid, UnknownSuite: f.Verdicts.UnknownSuite},
			Applied:  f.Applied, Carried: f.Carried, Unapplied: f.Unapplied,
			Counts: stateLine{
				Items: f.Counts.Items, Tombstoned: f.Counts.Tombstoned, Comments: f.Counts.Comments, Labels: f.Counts.Labels,
				ProseConflicts: f.Counts.ProseConflicts, Overruled: f.Counts.Overruled, MixedForms: f.Counts.MixedForms,
				Collisions: f.Counts.Collisions, Uncreated: f.Counts.Uncreated,
			},
		}
		l.Base, l.Ref, l.Why = base(f.Base)
		out = append(out, l)
	}
	return out
}

// base is where a fold started: extended, or genesis and why, with the
// stream or the error the why names.
func base(b fold.BaseFact) (name, ref, why string) {
	switch b := b.(type) {
	case fold.Extended:
		return "extended", "", ""
	case fold.Refolded:
		switch w := b.Why.(type) {
		case fold.Missed:
			return genesis(w.Miss)
		case fold.PayloadUnreadable:
			return "genesis:payload_unreadable", "", w.Err.Error()
		}
		panic(fmt.Sprintf("command: %T is not a Refold", b.Why))
	}
	panic(fmt.Sprintf("command: %T is not a BaseFact", b))
}

func genesis(m store.Miss) (name, ref, why string) {
	switch m := m.(type) {
	case store.Absent:
		return "genesis:absent", "", ""
	case store.Torn:
		return "genesis:torn", "", m.Why.Error()
	case store.TagChanged:
		return "genesis:tag_changed", "", ""
	case store.Rewritten:
		return "genesis:rewritten", string(m.Ref.Name()), ""
	case store.Withdrawn:
		return "genesis:withdrawn", string(m.Ref.Name()), ""
	}
	panic(fmt.Sprintf("command: %T is not a Miss", m))
}
