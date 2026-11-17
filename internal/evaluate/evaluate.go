package evaluate

import (
	"time"

	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

// View is the backlog as one reader sees it at one instant: the folded
// state, judged under the reader's file, at now. Stage and rollup derive
// here; held and expiry land here too.
type View struct {
	state    fold.State
	schema   config.Schema
	prefixes events.Prefixes
	now      time.Time
}

// Of is the view of s under schema at now, a pure function: evaluate reads the
// clock's value, never the clock (§8). Its prefixes are schema's and every
// one a version s holds declared, the fold keeping the bytes and config
// reading them (decision H).
func Of(s fold.State, schema config.Schema, now time.Time) View {
	var declared []events.Prefix
	switch w := s.Workspace().(type) {
	case fold.NoVersion:
	case fold.InForce:
		for _, v := range w.Applied {
			if p, ok := config.DeclaredPrefix(v.Bytes); ok {
				declared = append(declared, p)
			}
		}
	}
	return View{state: s, schema: schema, prefixes: events.KnownPrefixes(schema.Prefix(), declared...), now: now}
}

// Prefixes is every prefix an id may be typed under: the active version's
// and every one a version in the log declared (§3.2).
func (v View) Prefixes() events.Prefixes { return v.prefixes }

// State is the folded state the view derives from.
func (v View) State() fold.State { return v.state }

// Of is the same reader's view of s: its schema, at its now.
func (v View) Of(s fold.State) View { return Of(s, v.schema, v.now) }

// Schema is the reader's file the view judges under.
func (v View) Schema() config.Schema { return v.schema }
