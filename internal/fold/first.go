package fold

import (
	"fmt"

	"github.com/promptctl/lit2/internal/events"
)

// The names the first facts write under: an item's labels, its comments'
// list, and a comment's body.
var (
	labels   = mustName("labels")
	comments = mustName("comments")
	body     = mustName("body")
)

func mustName(s string) events.Name {
	n, err := events.ParseName(s)
	if err != nil {
		panic(err)
	}
	return n
}

// FirstFacts is the kinds of the first slice: items, fields, labels and
// comments, each a payload and one rule over the shapes.
func FirstFacts() []Kind {
	return []Kind{
		On(Items, func(f Fact[events.ItemCreate]) Applies {
			return Changes{List: []Change{CreateItem{ID: itemAt(f.At), Place: f.Payload.Place}}}
		}, events.ReadItemCreate),
		On(Items, func(f Fact[events.ItemDelete]) Applies {
			return Changes{List: []Change{Tombstone{Item: itemAt(f.At)}}}
		}, events.ReadItemDelete),
		On(Nodes, func(f Fact[events.FieldSet]) Applies {
			return Changes{List: []Change{Write{At: f.At, Field: f.Payload.Field, Value: f.Payload.Value, Over: f.Payload.Over}}}
		}, events.ReadFieldSet),
		On(Items, func(f Fact[events.LabelAdd]) Applies {
			return Changes{List: []Change{Add{At: f.At, Set: labels, Member: f.Payload.Label.Name()}}}
		}, events.ReadLabelAdd),
		On(Items, func(f Fact[events.LabelRm]) Applies {
			return Changes{List: []Change{Remove{At: f.At, Set: labels, Member: f.Payload.Label.Name(), Observed: f.Payload.Observed}}}
		}, events.ReadLabelRm),
		On(Elements, func(f Fact[events.CommentAdd]) Applies {
			el := elementAt(f.At)
			return Changes{List: []Change{
				CreateElement{Of: el.Of, Name: el.Name, In: comments},
				Write{At: el, Field: body, Value: events.Prose{Body: f.Payload.Body}},
			}}
		}, events.ReadCommentAdd),
	}
}

// itemAt is the item an Items kind's fact is about.
func itemAt(at Target) events.ItemID {
	switch at := at.(type) {
	case OnItem:
		return at.ID
	case OnElement:
	}
	panic(fmt.Sprintf("fold: an item kind's fact is about %#v", at))
}

// elementAt is the element an Elements kind's fact is about.
func elementAt(at Target) OnElement {
	switch at := at.(type) {
	case OnElement:
		return at
	case OnItem:
	}
	panic(fmt.Sprintf("fold: an element kind's fact is about %#v", at))
}
