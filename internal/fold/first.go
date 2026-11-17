package fold

import (
	"github.com/promptctl/lit2/internal/events"
)

// The names the first facts write under: an item's status, which only a
// transition writes and evaluate reads; its comments' list; and a comment's
// body, which verbs open.
var (
	Status   = events.MustName("status")
	comments = events.MustName("comments")
	Body     = events.MustName("body")
)

// FirstFacts is the kinds of the first slice: items, fields, members of a
// set, moves between columns and comments, each a payload and one rule
// over the shapes.
func FirstFacts() []Kind {
	return []Kind{
		On(NewItems, func(f Fact[events.ItemCreate]) Applies {
			return Changes{List: []Change{CreateItem{New: f.At.(OnNewItem).New, Place: f.Payload.Place}}}
		}, events.ReadItemCreate),
		On(Nodes, func(f Fact[events.FieldSet]) Applies {
			if f.Payload.Field == Status {
				// [LAW:one-source-of-truth] the status register is a
				// transition's alone, so it only ever holds a column's name
				return Misaddressed{Want: "a field other than status, which a transition writes"}
			}
			return Changes{List: []Change{Write{At: f.At.(Node), Field: f.Payload.Field, Value: f.Payload.Value, Over: f.Payload.Over}}}
		}, events.ReadFieldSet),
		On(Items, func(f Fact[events.MemberAdd]) Applies {
			return Changes{List: []Change{Add{At: f.At.(Node), Set: f.Payload.Field, Member: f.Payload.Member}}}
		}, events.ReadMemberAdd),
		On(Items, func(f Fact[events.MemberRm]) Applies {
			return Changes{List: []Change{Remove{At: f.At.(Node), Set: f.Payload.Field, Member: f.Payload.Member, Observed: f.Payload.Observed}}}
		}, events.ReadMemberRm),
		On(Items, func(f Fact[events.Transition]) Applies {
			return Changes{List: []Change{Write{At: f.At.(Node), Field: Status, Value: events.Plain{Canonical: f.Payload.To.String()}, Over: f.Payload.Over}}}
		}, events.ReadTransition),
		On(NewElements, func(f Fact[events.CommentAdd]) Applies {
			el := f.At.(OnNewElement).New
			return Changes{List: []Change{
				CreateElement{New: el, In: comments},
				Write{At: OnElement{Of: el.Of, ID: el.ID}, Field: Body, Value: events.Prose{Body: f.Payload.Body}},
			}}
		}, events.ReadCommentAdd),
	}
}
