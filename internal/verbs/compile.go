package verbs

import (
	"fmt"
	"slices"
	"strings"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/field"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/tick"
)

// Change is what one write does, as the echo shows it. A compile function
// returns it beside its payloads and never renders it.
//
//sumtype:decl
type Change interface{ change() }

// Wrote is a register written: what it read before, what it reads after.
// Prose is whether its values are text, which the echo quotes.
type Wrote struct {
	Who    Who
	Path   field.Path
	Prose  bool
	Before []string
	After  string
	Source Source
}

// Added is a member added to a set.
type Added struct {
	Who    Who
	Path   field.Path
	Member string
	Source Source
}

// Removed is a member removed from a set.
type Removed struct {
	Who    Who
	Path   field.Path
	Member string
}

// Commented is a comment created in the list at Path, by the draft that
// creates it, whose minted element names it.
type Commented struct {
	Who   Who
	Path  field.Path
	Draft int
}

// Filed is the Nth new item, by the draft that creates it, and the
// container it is placed in.
type Filed struct {
	N      int
	Draft  int
	Parent string
}

func (Wrote) change()     {}
func (Added) change()     {}
func (Removed) change()   {}
func (Commented) change() {}
func (Filed) change()     {}

// compile is one write compiled by its shape's one function. Each reads
// the subject's facts and ancestry and never which kind of subject it is;
// what it writes over is named, for the twice pass, only for an item that
// exists, since what a create writes is new by construction.
func compile(w World, wr write) ([]piece, []Change, []claim, *refusal) {
	var pieces []piece
	var changes []Change
	var keys []claim
	var r *refusal
	switch wr.shape {
	case setOne:
		pieces, changes, r = one(w, wr)
		keys = []claim{{path: wr.path}}
	case addMember:
		var members []events.Name
		pieces, changes, members, r = add(wr)
		for _, m := range members {
			keys = append(keys, claim{path: wr.path, member: m.String()})
		}
	case rmMember:
		pieces, changes, r = remove(wr)
		keys = []claim{{path: wr.path, member: wr.value.(field.Scalar).Text}}
	case setMembers:
		pieces, changes, r = replace(wr)
		keys = []claim{{path: wr.path}} // the whole set, so it meets every member's claim
	case addComment:
		pieces, changes, r = comment(wr)
		keys = nil
	default:
		panic(fmt.Sprintf("verbs: shape %d compiles no write", wr.shape))
	}
	if r != nil {
		return nil, nil, nil, r
	}
	e, ok := wr.subject.At.(Existing)
	if !ok {
		return pieces, changes, nil, nil
	}
	for i := range keys {
		keys[i].who = e.ID
	}
	return pieces, changes, keys, nil
}

// one sets a register. A status is a move between the columns of the
// graph the subject is judged under; any other register a field.set, of
// prose for text and of the canonical value otherwise. Over is every live
// write of the register, so the write replaces what it read and leaves a
// write it did not read live beside it. A write that changes nothing,
// the field already reading exactly this value by at most one live write,
// drafts nothing.
func one(w World, wr write) ([]piece, []Change, *refusal) {
	s, ok := wr.value.(field.Scalar)
	if !ok {
		return nil, nil, refuse("value", "%s holds one value", wr.path)
	}
	canon, err := wr.decl.Kind.Parse(s.Text)
	if err != nil {
		return nil, nil, refuse("value", "%s: %v", wr.path, err)
	}
	facts := wr.subject.Facts
	name := events.MustName(string(wr.path))
	live := facts.Live(name)
	if _, column := wr.decl.Kind.(field.Column); column {
		eff, err := effective(w.Schema, wr.subject.In)
		if err != nil {
			return nil, nil, refuse("configuration", "%v", err)
		}
		col, ok := eff.Columns.Resolve(canon)
		if !ok {
			return nil, nil, refuse("value", "%s is not a column this item can be in; the columns are %s", canon, strings.Join(eff.Columns.Names(), ", "))
		}
		from := tick.StatusIn(eff, facts)
		if from == col.Name() && len(live) <= 1 {
			return nil, nil, nil
		}
		t := events.Transition{From: events.MustName(from), To: events.MustName(col.Name()), Over: live}
		return []piece{fact{t}}, []Change{Wrote{Who: wr.subject.At, Path: wr.path, Before: []string{from}, After: col.Name(), Source: wr.source}}, nil
	}
	before, _ := stored(w.Schema, facts, wr.subject.In, wr.path, wr.decl) // no status, so nothing the configuration judges
	if len(before) == 1 && before[0] == canon && len(live) <= 1 {
		return nil, nil, nil
	}
	_, prose := wr.decl.Kind.(field.Text)
	var v events.Value = events.Plain{Canonical: canon}
	if prose {
		v = events.Prose{Body: events.SealPlain(canon)}
	}
	set := events.FieldSet{Field: name, Value: v, Over: live}
	return []piece{fact{set}}, []Change{Wrote{Who: wr.subject.At, Path: wr.path, Prose: prose, Before: before, After: canon, Source: wr.source}}, nil
}

// add adds each member a value names: one, or a list. Each is read by the
// set's kind as a name, a member named twice in one value is refused, and
// one the subject already holds changes nothing.
func add(wr write) ([]piece, []Change, []events.Name, *refusal) {
	var texts []string
	switch v := wr.value.(type) {
	case field.Scalar:
		texts = []string{v.Text}
	case field.List:
		for _, it := range v.Items {
			s, ok := it.(field.Scalar)
			if !ok {
				return nil, nil, nil, refuse("value", "a member of %s is one value", wr.path)
			}
			texts = append(texts, s.Text)
		}
	default:
		return nil, nil, nil, refuse("value", "%s takes a member or a list of them", wr.path)
	}
	name := events.MustName(string(wr.path))
	held := wr.subject.Facts.Members(name)
	var pieces []piece
	var changes []Change
	var members []events.Name
	for _, t := range texts {
		canon, err := wr.decl.Kind.Parse(t)
		if err != nil {
			return nil, nil, nil, refuse("value", "%s: %v", wr.path, err)
		}
		m, err := events.ParseName(canon)
		if err != nil {
			return nil, nil, nil, refuse("value", "a member of %s is a name: %v", wr.path, err)
		}
		if slices.Contains(members, m) {
			return nil, nil, nil, refuse("twice", "%s/%s is written twice in this command", wr.path, m)
		}
		members = append(members, m)
		if slices.ContainsFunc(held, func(h fold.Member) bool { return h.Name == m }) {
			continue
		}
		pieces = append(pieces, fact{events.MemberAdd{Field: name, Member: m}})
		changes = append(changes, Added{Who: wr.subject.At, Path: wr.path, Member: m.String(), Source: wr.source})
	}
	return pieces, changes, members, nil
}

// remove removes a member. A held one is removed whatever the version now
// declares, so a label dropped from the file can come off; one not held
// that the kind admits changes nothing; and one the kind refuses is a
// value refused, which is how a typo shows.
func remove(wr write) ([]piece, []Change, *refusal) {
	text := wr.value.(field.Scalar).Text
	name := events.MustName(string(wr.path))
	m := events.MustName(text)
	for _, h := range wr.subject.Facts.Members(name) {
		if h.Name != m {
			continue
		}
		observed := make([]events.EventID, len(h.Added))
		for i, s := range h.Added {
			observed[i] = s.Event
		}
		return []piece{fact{events.MemberRm{Field: name, Member: m, Observed: observed}}}, []Change{Removed{Who: wr.subject.At, Path: wr.path, Member: text}}, nil
	}
	if _, err := wr.decl.Kind.Parse(text); err != nil {
		return nil, nil, refuse("value", "%s: %v", wr.path, err)
	}
	return nil, nil, nil
}

// replace makes a set hold exactly the members a value names: each one it
// does not hold added, as add adds it, and each it holds that the value
// does not name removed, as remove removes it.
func replace(wr write) ([]piece, []Change, *refusal) {
	pieces, changes, named, r := add(wr)
	if r != nil {
		return nil, nil, r
	}
	for _, h := range wr.subject.Facts.Members(events.MustName(string(wr.path))) {
		if slices.Contains(named, h.Name) {
			continue
		}
		rm := wr
		rm.value = field.Scalar{Text: h.Name.String()}
		ps, chs, _ := remove(rm) // a held member is removed whatever its kind reads
		pieces, changes = append(pieces, ps...), append(changes, chs...)
	}
	return pieces, changes, nil
}

// comment creates one comment: one non-empty text.
func comment(wr write) ([]piece, []Change, *refusal) {
	s, ok := wr.value.(field.Scalar)
	if !ok {
		return nil, nil, refuse("value", "a comment is one text")
	}
	text, err := wr.decl.Kind.Parse(s.Text)
	if err != nil {
		return nil, nil, refuse("value", "a comment: %v", err)
	}
	return []piece{newElement{events.CommentAdd{Body: events.SealPlain(text)}}}, []Change{Commented{Who: wr.subject.At, Path: wr.path}}, nil
}
