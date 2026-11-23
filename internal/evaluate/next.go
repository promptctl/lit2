package evaluate

import (
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

// Next is what /next serves: the head it settled on, every lane passed
// over before it with why, and how many lanes the walk visited, so an
// empty backlog and a passed-over one do not read alike.
type Next struct {
	Head       Head
	Passed     []Passed
	Considered int
}

// Head is the leaf /next serves, or none.
//
//sumtype:decl
type Head interface{ head() }

// Served is the leaf /next serves.
type Served struct{ ID events.ItemID }

// NoHead is a walk that served nothing.
type NoHead struct{}

func (Served) head() {}
func (NoHead) head() {}

// Passed is a lane whose head /next would not serve, and why.
type Passed struct {
	Head events.ItemID
	Why  Why
}

// Why is the reason a head is passed over.
//
//sumtype:decl
type Why interface{ why() }

// Undeclared is a head in a column its graph no longer declares
// (specifications/columns-and-requirements.md, "Validity of a column graph").
type Undeclared struct{ Column string }

func (Undeclared) why() {}

// Next walks the lanes in rank order and serves the first head it can
// (specifications/lanes-and-claims.md, "Lanes"): a lane's head is its first member rm has not
// removed, itself or above it, and whose stage is not terminal, and a lane with none is
// finished. Claims and held refine the walk in slice 4 without changing
// its path. A graph the configuration refuses is its refusal.
func (v View) Next() (Next, error) {
	n := Next{Head: NoHead{}}
	for _, lane := range v.state.Lanes() {
		n.Considered++
		for _, id := range lane.Members {
			if v.Gone(id) {
				continue
			}
			c, err := v.Column(id)
			if err != nil {
				return Next{}, err
			}
			if c.Stage == config.Terminal {
				continue
			}
			if !c.Declared {
				n.Passed = append(n.Passed, Passed{Head: id, Why: Undeclared{Column: c.Name}})
				break
			}
			n.Head = Served{ID: id}
			return n, nil
		}
	}
	return n, nil
}

// Gone is whether rm removed id or a node above it: a removed subtree is
// no work, as Stage and Rollup read it, and every view marks it so.
func (v View) Gone(id events.ItemID) bool { return gone(v.state, id) }

func gone(st fold.State, id events.ItemID) bool {
	if Removed(st.Item(id).(fold.Found).Item.Facts) {
		return true
	}
	switch in := st.Container(id).(type) {
	case fold.Under:
		return gone(st, in.Parent)
	case fold.Home:
	}
	return false
}
