package config

import (
	"errors"
	"fmt"

	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/git"
)

// ActiveConfig is the version the log says is in force. A committed version
// is recorded in the log by `config commit`; with none recorded, the shipped
// default profile is the model in force (architecture.md §3.15).
type ActiveConfig interface{ isActiveConfig() }

// Committed is the latest version the log holds, as its bytes.
type Committed struct{ Bytes []byte }

// NeverCommitted is a workspace with no version ever committed.
type NeverCommitted struct{}

func (Committed) isActiveConfig()      {}
func (NeverCommitted) isActiveConfig() {}

// Source says where the active version came from.
type Source interface{ isSource() }

// FromLog is a version a person committed.
type FromLog struct{}

// FromDefault is the shipped default profile.
type FromDefault struct{}

func (FromLog) isSource()     {}
func (FromDefault) isSource() {}

// Loaded is the active version, parsed once, with the bytes its config-ref
// names. It is the one value a command receives from configuration: a
// consumer reads its Schema and never parses a version itself.
type Loaded struct {
	schema Schema
	source Source
	bytes  []byte
}

// Load parses the active version under the running reader. It reads nothing
// but its argument: a draft in the working tree is not an input, because only
// a committed version is in force.
func Load(a ActiveConfig, running ReaderVersion) (Loaded, error) {
	switch a := a.(type) {
	case NeverCommitted:
		b := assets.DefaultWorkspace.Bytes()
		s, err := Parse(b, "the default profile", running)
		if err != nil {
			return Loaded{}, err
		}
		return Loaded{schema: s, source: FromDefault{}, bytes: b}, nil
	case Committed:
		s, err := Parse(a.Bytes, "the committed workspace version", running)
		if err != nil {
			return Loaded{}, err
		}
		return Loaded{schema: s, source: FromLog{}, bytes: a.Bytes}, nil
	}
	panic(fmt.Sprintf("config: %T is not an ActiveConfig", a))
}

// Schema is the parsed version.
func (l Loaded) Schema() Schema { return l.schema }

// Source is where the version came from.
func (l Loaded) Source() Source { return l.source }

// Ref is the id git gives the version's bytes in the code repository's format:
// the config-ref an event records, which names the version it was written under.
func (l Loaded) Ref(f git.ObjectFormat) git.ObjectID { return git.HashBlob(f, l.bytes) }

// Facts is what the command event records of the configuration a run read,
// `[LAW:nothing-unseen]`. Zero counts are facts, so every count is present.
type Facts struct {
	Source   string
	Gate     string
	Outcome  string
	Refusals []Refusal
	Counts   Counts
}

// Counts is how many of each declaration the version holds.
type Counts struct {
	Levels, Columns, Labels, Requirements, Checks, Links, Sections int
}

// Facts reads a loaded version's facts for the command event.
func (l Loaded) Facts() Facts {
	s := l.schema
	reqs := 0
	for _, name := range s.root.Columns.Names() {
		c, _ := s.root.Columns.Resolve(name)
		reqs += len(c.Entry()) + len(c.Exit())
	}
	src := "default"
	if _, ok := l.source.(FromLog); ok {
		src = "log"
	}
	gate := "none"
	if g, ok := s.gate.(AtLeast); ok {
		gate = g.V
	}
	return Facts{
		Source:  src,
		Gate:    gate,
		Outcome: "parsed",
		Counts: Counts{
			Levels:       len(s.levels),
			Columns:      len(s.root.Declared),
			Labels:       len(s.labels.Values()),
			Requirements: reqs,
			Checks:       len(s.checks),
			Links:        len(s.links),
			Sections:     len(s.sections),
		},
	}
}

// RefusedFacts reads the facts of a version that was refused: its source and
// every fault in it. The counts are zero, because nothing parsed.
func RefusedFacts(err error, a ActiveConfig) Facts {
	src := "default"
	if _, ok := a.(Committed); ok {
		src = "log"
	}
	f := Facts{Source: src, Gate: "none", Outcome: "refused"}
	var r *Refused
	if errors.As(err, &r) {
		f.Refusals = r.Refusals
	}
	return f
}
