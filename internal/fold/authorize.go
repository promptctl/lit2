package fold

import (
	"fmt"
	"strings"

	"github.com/promptctl/lit2/internal/events"
)

// Intent is what an event does, in the words of the path surface: a write
// of a verb to a path, or an act the policy rule judges. Every kind
// declares its own at its On line, so a kind that writes and names no
// path does not compile `[LAW:types-are-the-program]`.
//
//sumtype:decl
type Intent interface{ intent() }

// PathWrite is a write of Verb to the path At/Field: the operand the verbs
// compiled it from, read back (specifications/access.md, "Capabilities and
// roles").
type PathWrite struct {
	Verb  events.Verb
	At    Subject
	Field []string
}

// Governed is a policy event's Act, which the policy rule judges against
// the version in force and applies.
type Governed struct{ Act PolicyAct }

func (PathWrite) intent() {}
func (Governed) intent()  {}

// String is the path as a person writes it: the verb, then the path from
// the subject.
func (w PathWrite) String() string {
	path := w.At.String()
	if len(w.Field) > 0 {
		path = strings.TrimSuffix(path, "/") + "/" + strings.Join(w.Field, "/")
	}
	return fmt.Sprintf("%s %s", w.Verb, path)
}

// Subject is what an intent path starts from: the root, a level's home,
// an item, or an element of an item: the node an own binding reads.
//
//sumtype:decl
type Subject interface {
	subject()
	String() string
}

// OfRoot is the root, /.
type OfRoot struct{}

// OfHome is the home of the parentless items at level Holds.
type OfHome struct{ Holds events.Level }

// OfItem is an item.
type OfItem struct{ ID events.ItemID }

// OfElement is element ID of item Of: its path is the item's, then its
// list and its id.
type OfElement struct {
	Of events.ItemID
	ID events.ElementID
}

func (OfRoot) subject()    {}
func (OfHome) subject()    {}
func (OfItem) subject()    {}
func (OfElement) subject() {}

func (OfRoot) String() string   { return "/" }
func (h OfHome) String() string { return fmt.Sprintf("(home of level %d)", h.Holds.Int()) }
func (i OfItem) String() string { return i.ID.String() }
func (e OfElement) String() string {
	return e.Of.String() + "/" + comments.String() + "/" + e.ID.String()
}

// steps is w's path below its item, which a capability's glob matches:
// an element's list and id first.
func (w PathWrite) steps() []string {
	if e, ok := w.At.(OfElement); ok {
		return append([]string{comments.String(), e.ID.String()}, w.Field...)
	}
	return w.Field
}

// Authorization is the capability verdict on one path write.
//
//sumtype:decl
type Authorization interface{ authorization() }

// Granted is a write the writer's role allows, By the grant that matched.
type Granted struct {
	Role events.Name
	By   Grant
}

// Refused is a write no grant of the writer's standing allows. Misses is
// each capability of the write's verb the role holds, with what it failed,
// so a refusal says what it tried `[LAW:nothing-unseen]`.
type Refused struct {
	As     Standing
	Misses []Miss
}

func (Granted) authorization() {}
func (Refused) authorization() {}

// Grant is what allowed an event.
//
//sumtype:decl
type Grant interface{ grant() }

// OwnerSurface is the owner role, which grants every path.
type OwnerSurface struct{}

// ByCapability is one capability of the writer's role.
type ByCapability struct{ Capability events.Capability }

// ByPolicyRule is a policy act the policy rule allowed.
type ByPolicyRule struct{}

func (OwnerSurface) grant() {}
func (ByCapability) grant() {}
func (ByPolicyRule) grant() {}

// Standing is who the writer is under the policy: a member in a role, or
// no member.
//
//sumtype:decl
type Standing interface{ standing() }

// AsRole is a member holding Role.
type AsRole struct{ Role events.Name }

// NotAMember is a principal the policy does not list.
type NotAMember struct{}

func (AsRole) standing()     {}
func (NotAMember) standing() {}

// Miss is one capability that did not allow a write, and every part of it
// the write failed.
type Miss struct {
	Capability events.Capability
	Failed     []Failure
}

// Failure is one part of a capability a write does not meet.
//
//sumtype:decl
type Failure interface{ failure() }

// PathOutside is a path the capability's glob does not cover; a root path
// is outside every capability, which matches items and homes alone.
type PathOutside struct{}

// BindingUnmet is a subject the capability's binding does not match: an
// item the writer did not create, for own.
type BindingUnmet struct{}

// ConditionUnmet is a subject that does not hold Condition.
type ConditionUnmet struct{ Condition events.Condition }

func (PathOutside) failure()    {}
func (BindingUnmet) failure()   {}
func (ConditionUnmet) failure() {}

// authorize is p's verdict on who writing w over s, the state before the
// write: the owner role is granted every path; any other member, the first
// capability of its role that covers the path, matches the subject and
// holds its conditions. Every input is a fact the fold holds, so every
// verifier reaches one verdict `[LAW:single-enforcer]`.
func (s State) authorize(p events.Policy, who events.PrincipalID, w PathWrite) Authorization {
	role, ok := p.Role(who)
	switch {
	case !ok:
		return Refused{As: NotAMember{}}
	case role == p.Owner():
		return Granted{Role: role, By: OwnerSurface{}}
	}
	var misses []Miss
	for _, c := range p.Grants(role) {
		if c.Verb != w.Verb {
			continue
		}
		failed := s.fails(who, c, w)
		if len(failed) == 0 {
			return Granted{Role: role, By: ByCapability{Capability: c}}
		}
		misses = append(misses, Miss{Capability: c, Failed: failed})
	}
	return Refused{As: AsRole{Role: role}, Misses: misses}
}

// fails is every part of c that who writing w does not meet.
func (s State) fails(who events.PrincipalID, c events.Capability, w PathWrite) []Failure {
	if _, root := w.At.(OfRoot); root {
		return []Failure{PathOutside{}}
	}
	var out []Failure
	if !c.Field.Matches(w.steps()) {
		out = append(out, PathOutside{})
	}
	if !s.binds(c.Binding, who, w.At) {
		out = append(out, BindingUnmet{})
	}
	for _, cond := range c.Where {
		if !s.holds(cond, w.At) {
			out = append(out, ConditionUnmet{Condition: cond})
		}
	}
	return out
}

// binds is whether b matches at for who: every item and home for all; a
// node who created for own, so a comment is its author's whoever created
// its item. No holder is folded yet, so assignee matches
// nothing and own-or-assignee what own matches.
func (s State) binds(b events.Binding, who events.PrincipalID, at Subject) bool {
	switch b {
	case events.All:
		return true
	case events.Own, events.OwnOrAssignee:
		creator, ok := s.Creator(at)
		return ok && creator == who
	case events.Assignee:
		return false
	}
	panic(fmt.Sprintf("fold: %d is not a Binding", uint8(b)))
}

// holds is whether at holds c: a home and an element have no level, so a
// level condition holds only on a leveled item.
func (s State) holds(c events.Condition, at Subject) bool {
	switch c := c.(type) {
	case events.LevelIs:
		it, ok := at.(OfItem)
		if !ok {
			return false
		}
		l, ok := s.levelOf(it.ID).(Leveled)
		return ok && (l.Level == c.Level) == (c.Op == events.Eq)
	}
	panic(fmt.Sprintf("fold: %T is not a Condition", c))
}

// Creator is the principal whose create placed the node at, and false for
// a home, the root, or a node nothing created.
func (s State) Creator(at Subject) (events.PrincipalID, bool) {
	var o Origin = Uncreated{}
	switch at := at.(type) {
	case OfRoot, OfHome:
	case OfItem:
		if r, ok := s.items.get(at.ID); ok {
			o = r.origin()
		}
	case OfElement:
		if r, ok := s.items.get(at.Of); ok {
			if el, ok := r.elements.get(at.ID); ok {
				o = el.origin(at.Of)
			}
		}
	}
	c, ok := CreateOf(o)
	return c.Stamp.By.Principal, ok
}
