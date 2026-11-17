package fold

import (
	"bytes"
	"slices"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/git"
)

// versions is the workspace's applied versions (§3.15): every apply
// folded, kept whole for the prefixes a version once declared, and the
// applies some apply named in Over. It is the register's rule over
// versions: an apply retires what it names, and the rest stay live.
type versions struct {
	applied table[events.EventID, applyRec, byEvent]
	retired idSet
}

// applyRec is one apply: its stamp and the version it carries.
type applyRec struct {
	stamp Stamp
	blob  git.ObjectID
	bytes []byte
}

func (a applyRec) made() Stamp         { return a.stamp }
func (a applyRec) key() events.EventID { return a.stamp.Event }

func (a applyRec) version() Version { return Version{Stamp: a.stamp, Blob: a.blob, Bytes: a.bytes} }

// with is v with st's apply of c folded in.
func (v versions) with(st Stamp, c Apply) versions {
	v.retired = withIDs(v.retired, c.Over)
	v.applied = v.applied.set(applyRec{stamp: st, blob: c.Blob, bytes: c.Bytes})
	return v
}

// Version is one applied version: the apply that carried it, git's id
// for its bytes, and the bytes. The fold reads none of them (decision H).
type Version struct {
	Stamp Stamp
	Blob  git.ObjectID
	Bytes []byte
}

// Workspace is which version is in force, as the folded applies say.
//
//sumtype:decl
type Workspace interface{ workspace() }

// NoVersion is a store no live apply is in: one made before an apply was
// the first event a store holds, or empty.
type NoVersion struct{}

// InForce is the version every command runs on, Active, the last live
// apply in total order, so every replica holding the same events runs the
// same one. Contending is every other live apply holding another blob,
// in total order: two applies that named one parent, a conflict until an
// apply names them all. Two live applies of one blob agree. Live is every
// live apply, what the next apply names in Over; Applied is every apply
// folded, in total order.
type InForce struct {
	Active     Version
	Contending []Version
	Live       []events.EventID
	Applied    []Version
}

func (NoVersion) workspace() {}
func (InForce) workspace()   {}

// Workspace is the version in force.
func (s State) Workspace() Workspace {
	live := liveOf(s.workspace.applied, s.workspace.retired)
	if len(live) == 0 {
		return NoVersion{}
	}
	active := live[len(live)-1].version()
	in := InForce{Active: active}
	for _, a := range live {
		in.Live = append(in.Live, a.key())
		if a.blob != active.Blob {
			in.Contending = append(in.Contending, a.version())
		}
	}
	for _, a := range s.workspace.applied.sorted(byMade[applyRec]) {
		in.Applied = append(in.Applied, a.version())
	}
	return in
}

func (v versions) equal(o versions) bool {
	return slices.Equal(sortedIDs(v.retired), sortedIDs(o.retired)) &&
		slices.EqualFunc(v.applied.sorted(byMade[applyRec]), o.applied.sorted(byMade[applyRec]), func(a, b applyRec) bool {
			return a.stamp.equal(b.stamp) && a.blob == b.blob && bytes.Equal(a.bytes, b.bytes)
		})
}
