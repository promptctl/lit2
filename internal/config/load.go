package config

import (
	"github.com/BurntSushi/toml"
	"github.com/promptctl/lit2/internal/events"
)

// Load parses a version's bytes under the running reader: the active
// version the log holds, or one a person proposes to apply. It reads
// nothing but its arguments, so a draft in the working tree is never an
// input (specifications/workspace-file.md, "Applying a version").
func Load(b []byte, running ReaderVersion) (Schema, error) {
	return Parse(b, "the workspace version", running)
}

// DeclaredPrefix is the prefix a version's bytes declare, read alone: an
// applied version, whose prefix every id once rendered under still parses
// under (specifications/events-and-store.md, "Ids"). A version parsed when it was applied, so its prefix reads;
// false is bytes with none, which no apply carries.
func DeclaredPrefix(b []byte) (events.Prefix, bool) {
	var v struct {
		Prefix string `toml:"prefix"`
	}
	if _, err := toml.Decode(string(b), &v); err != nil {
		return events.Prefix{}, false
	}
	p, err := events.ParsePrefix(v.Prefix)
	return p, err == nil
}

// Facts is what the command event records of the configuration a run read,
// `[LAW:nothing-unseen]`. Zero counts are facts, so every count is present.
type Facts struct {
	Gate   string
	Counts Counts
}

// Counts is how many of each declaration the version holds. Columns and
// requirements count the root's and every section's.
type Counts struct {
	Levels, Columns, Labels, Requirements, Checks, Links, Sections int
}

// Facts reads a version's facts for the command event.
func (s Schema) Facts() Facts {
	cols, reqs := 0, 0
	count := func(cs map[string]Column) {
		cols += len(cs)
		for _, c := range cs {
			reqs += len(c.Entry()) + len(c.Exit())
		}
	}
	count(s.root.Declared)
	for _, sec := range s.sections {
		count(sec.Columns)
	}
	gate := "none"
	if g, ok := s.gate.(AtLeast); ok {
		gate = g.V
	}
	return Facts{
		Gate: gate,
		Counts: Counts{
			Levels:       len(s.levels),
			Columns:      cols,
			Labels:       len(s.labels.Values()),
			Requirements: reqs,
			Checks:       len(s.checks),
			Links:        len(s.links),
			Sections:     len(s.sections),
		},
	}
}
