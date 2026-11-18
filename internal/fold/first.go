package fold

import (
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/events/wire"
)

// The names the first facts write under: an item's status, which only a
// transition writes and evaluate reads; its comments' list; a comment's
// body, which verbs open; and the flag rm sets, which the policy judges as
// the rm it is.
var (
	Status   = events.MustName("status")
	comments = events.MustName("comments")
	Body     = events.MustName("body")
	Deleted  = events.MustName("deleted")
	children = "children"
)

// FirstFacts is the kinds of the first slice: items, fields, members of a
// set, moves between columns, comments, the workspace's versions and the
// policy, each a payload, one rule over the shapes, and the path write it
// is the inverse of `[LAW:one-source-of-truth]`.
func FirstFacts() []Kind {
	return []Kind{
		On(NewItems, func(f Fact[events.ItemCreate]) Applies {
			return Changes{List: []Change{CreateItem{New: f.At.(OnNewItem).New, Place: f.Payload.Place}}}
		}, func(f Fact[events.ItemCreate]) Intent {
			// a child create's subject is its parent, named only in its
			// placement; a top-level item's is its level's home
			switch p := f.Payload.Place.(type) {
			case events.BelowParent:
				return PathWrite{Verb: events.Add, At: OfItem{ID: p.Parent}, Field: []string{children}}
			case events.AtLevel:
				return PathWrite{Verb: events.Add, At: OfHome{Holds: p.Level}, Field: []string{children}}
			}
			panic("fold: a create placed nowhere decoded")
		}, events.ReadItemCreate),
		On(Nodes, func(f Fact[events.FieldSet]) Applies {
			if f.Payload.Field == Status {
				// [LAW:one-source-of-truth] the status register is a
				// transition's alone, so it only ever holds a column's name
				return Misaddressed{Want: "a field other than status, which a transition writes"}
			}
			return Changes{List: []Change{Write{At: f.At.(Node), Field: f.Payload.Field, Value: f.Payload.Value, Over: f.Payload.Over}}}
		}, func(f Fact[events.FieldSet]) Intent {
			// rm x is the write of its deleted flag, so any write of the
			// flag is judged as the rm it is: restoring a node is removing
			// it undone, which only who may remove it may do
			at := subjectOf(f.At.(Node))
			if f.Payload.Field == Deleted {
				return PathWrite{Verb: events.Rm, At: at}
			}
			return PathWrite{Verb: events.Set, At: at, Field: []string{f.Payload.Field.String()}}
		}, events.ReadFieldSet),
		On(Items, func(f Fact[events.MemberAdd]) Applies {
			return Changes{List: []Change{Add{At: f.At.(Node), Set: f.Payload.Field, Member: f.Payload.Member}}}
		}, func(f Fact[events.MemberAdd]) Intent {
			return PathWrite{Verb: events.Add, At: OfItem{ID: f.At.(OnItem).ID}, Field: []string{f.Payload.Field.String()}}
		}, events.ReadMemberAdd),
		On(Items, func(f Fact[events.MemberRm]) Applies {
			return Changes{List: []Change{Remove{At: f.At.(Node), Set: f.Payload.Field, Member: f.Payload.Member, Observed: f.Payload.Observed}}}
		}, func(f Fact[events.MemberRm]) Intent {
			return PathWrite{Verb: events.Rm, At: OfItem{ID: f.At.(OnItem).ID}, Field: []string{f.Payload.Field.String(), f.Payload.Member.String()}}
		}, events.ReadMemberRm),
		On(Items, func(f Fact[events.Transition]) Applies {
			return Changes{List: []Change{Write{At: f.At.(Node), Field: Status, Value: events.Plain{Canonical: f.Payload.To.String()}, Over: f.Payload.Over}}}
		}, func(f Fact[events.Transition]) Intent {
			return PathWrite{Verb: events.Set, At: OfItem{ID: f.At.(OnItem).ID}, Field: []string{Status.String()}}
		}, events.ReadTransition),
		On(NewElements, func(f Fact[events.CommentAdd]) Applies {
			el := f.At.(OnNewElement).New
			return Changes{List: []Change{
				CreateElement{New: el, In: comments},
				Write{At: OnElement{Of: el.Of, ID: el.ID}, Field: Body, Value: events.Prose{Body: f.Payload.Body}},
			}}
		}, func(f Fact[events.CommentAdd]) Intent {
			return PathWrite{Verb: events.Add, At: OfItem{ID: f.At.(OnNewElement).New.Of}, Field: []string{comments.String()}}
		}, events.ReadCommentAdd),
		On(Root, func(f Fact[events.WorkspaceApply]) Applies {
			return Changes{List: []Change{Apply{Blob: f.Payload.Blob(), Bytes: f.Payload.Version(), Over: f.Payload.Over}}}
		}, func(Fact[events.WorkspaceApply]) Intent {
			return PathWrite{Verb: events.Set, At: OfRoot{}, Field: []string{"workspace"}}
		}, events.ReadWorkspaceApply),
		governs(func(p events.Enroll) PolicyAct { return EnrollPolicy{Enroll: p} }, events.ReadEnroll),
		governs(func(p events.Propose) PolicyAct { return ProposePolicy{Propose: p} }, events.ReadPropose),
		governs(func(p events.Approve) PolicyAct { return ApprovePolicy{Proposal: p.Proposal} }, events.ReadApprove),
		governs(func(events.Request) PolicyAct { return RequestMembership{} }, events.ReadRequest),
	}
}

// governs is a policy kind, aimed at the root: its act is its intent,
// which the policy rule judges and applies, so it changes no shape.
func governs[P events.Payload](act func(P) PolicyAct, decode func(*wire.Reader) (events.Versioned, error)) Kind {
	return On(Root, func(Fact[P]) Applies { return Changes{} },
		func(f Fact[P]) Intent { return Governed{Act: act(f.Payload)} }, decode)
}

// subjectOf is the subject node n is: an item, or an element.
func subjectOf(n Node) Subject {
	switch n := n.(type) {
	case OnItem:
		return OfItem(n)
	case OnElement:
		return OfElement(n)
	}
	panic("fold: a node is an item or an element")
}
