package evaluate

import (
	"time"

	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/fold"
)

// View is the backlog as one reader sees it at one instant: the folded
// state, judged under the reader's file, at now. Stage and rollup derive
// here; held and expiry land here too.
type View struct {
	state  fold.State
	schema config.Schema
	now    time.Time
}

// Of is the view of s under schema at now, a pure function: evaluate reads the
// clock's value, never the clock (§8).
func Of(s fold.State, schema config.Schema, now time.Time) View {
	return View{state: s, schema: schema, now: now}
}

// State is the folded state the view derives from.
func (v View) State() fold.State { return v.state }

// Schema is the reader's file the view judges under.
func (v View) Schema() config.Schema { return v.schema }
