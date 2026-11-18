package verbs

import (
	"fmt"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

// Owner is the role that founds a store and holds every path.
var Owner = events.MustName("owner")

// DefaultRoles is the default profile's roles below owner, spelled as path
// globs (access-control/design.md, Roles): observer writes nothing;
// contributor files work, moves any ticket, comments, and edits and
// removes what it created, removing only a leaf of its items; editor writes and removes every path of every item. A
// column condition the fold cannot judge from its own facts is no
// capability's, so contributor's status moves are bound all.
func DefaultRoles() map[events.Name][]events.Capability {
	contributor := capabilities("add */children", "add own/*", "set own/*", "set */status", "add */comments",
		"rm own/*/*", "rm own/* (level=1)")
	return map[events.Name][]events.Capability{
		events.MustName("observer"):    nil,
		events.MustName("contributor"): contributor,
		events.MustName("editor"):      append(capabilities("set */*", "add */*", "rm */*"), contributor...),
	}
}

func capabilities(texts ...string) []events.Capability {
	cs := make([]events.Capability, len(texts))
	for i, t := range texts {
		cs[i] = events.MustCapability(t)
	}
	return cs
}

// InitStep is what admin init does with this checkout's principal, by the
// policy the store's fold holds.
//
//sumtype:decl
type InitStep interface{ initStep() }

// Found is a store no enrollment founded: this principal enrolls it, its
// one owner.
type Found struct{ Enroll events.Enroll }

// Listed is a principal the policy in force lists, in Role.
type Listed struct{ Role events.Name }

// Ask is a principal the policy does not list and that has not asked:
// it requests membership, which an owner grants by adding it.
type Ask struct{}

// Asked is a principal that requested membership and is not yet a member.
type Asked struct{}

// FoundingAbsent is a store founded by an enrollment it does not hold yet:
// init can judge no request against it.
type FoundingAbsent struct{ Enrollment events.EventID }

// FoundedUnread is a store founded by an enrollment this release cannot read:
// init can neither adopt it nor found another.
type FoundedUnread struct{ Enrollment events.EventID }

func (Found) initStep()          {}
func (Listed) initStep()         {}
func (Ask) initStep()            {}
func (Asked) initStep()          {}
func (FoundingAbsent) initStep() {}
func (FoundedUnread) initStep()  {}

// Bootstrap is admin init's step for me over st, the local fold: a store
// whose checkout names its founding joins it, and only one that names none
// is founded.
func Bootstrap(st fold.State, me events.PrincipalID) InitStep {
	switch p := st.Policy().(type) {
	case fold.Unenrolled:
		return Found{Enroll: events.Enroll{Owner: Owner, Roles: DefaultRoles()}}
	case fold.FoundingAbsent:
		return FoundingAbsent(p)
	case fold.FoundedUnread:
		return FoundedUnread(p)
	case fold.Enrolled:
		if role, ok := p.Policy.Role(me); ok {
			return Listed{Role: role}
		}
		for _, r := range st.Requests() {
			if r.Principal == me {
				return Asked{}
			}
		}
		return Ask{}
	}
	panic(fmt.Sprintf("verbs: %T is not a PolicyInForce", st.Policy()))
}
