package config

import (
	"fmt"
	"strings"

	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// ReaderVersion is the running binary's version, read once from Go's build
// stamp. It has two variants, and an unversioned build is not one of them: a
// build with no stamp is refused before any command runs, so no layer past
// main meets a reader it cannot place `[LAW:types-are-the-program]`.
type ReaderVersion interface{ isReaderVersion() }

// Released is a tagged build, v0.4.0.
type Released struct{ V string }

// Development is a build between tags: a pseudo-version over its Base, the
// tag before it. Revision is the commit it was built from; Modified says the
// tree had changes the commit does not hold.
type Development struct {
	Base     string
	Revision string
	Modified bool
}

func (Released) isReaderVersion()    {}
func (Development) isReaderVersion() {}

// ReadReaderVersion parses the main module's version as Go stamps it. Go
// stamps `(devel)` when a build has no version, which lit2 refuses to run.
func ReadReaderVersion(stamp string) (ReaderVersion, error) {
	const instruction = "build from a git checkout with `go build ./cmd/lit2`, or install a tagged version with `go install github.com/promptctl/lit2/cmd/lit2@<version>`"
	if stamp == "" || stamp == "(devel)" {
		return nil, fmt.Errorf("this build has no version, so it cannot say which workspace versions it reads: %s", instruction)
	}
	v, modified := strings.CutSuffix(stamp, "+dirty")
	if !semver.IsValid(v) {
		return nil, fmt.Errorf("the build stamp %q is not a version: %s", stamp, instruction)
	}
	if !module.IsPseudoVersion(v) {
		return Released{V: v}, nil
	}
	base, err := module.PseudoVersionBase(v)
	if err != nil {
		return nil, fmt.Errorf("the build stamp %q has no base version: %w", stamp, err)
	}
	rev, err := module.PseudoVersionRev(v)
	if err != nil {
		return nil, fmt.Errorf("the build stamp %q has no revision: %w", stamp, err)
	}
	return Development{Base: base, Revision: rev, Modified: modified}, nil
}

// String is the reader's version as a person reads it.
func String(r ReaderVersion) string {
	switch r := r.(type) {
	case Released:
		return r.V
	case Development:
		s := r.Base + " (development, " + r.Revision
		if r.Modified {
			s += ", modified"
		}
		return s + ")"
	}
	panic(fmt.Sprintf("config: %T is not a ReaderVersion", r))
}

// ReaderGate is the oldest reader a version allows: none, or a version it
// must be at least.
type ReaderGate interface{ isGate() }

// NoGate is a version that any reader may read; it is what an absent
// min-reader-version means.
type NoGate struct{}

// AtLeast is a gate a reader must be at least. V is a canonical semver.
type AtLeast struct{ V string }

func (NoGate) isGate()  {}
func (AtLeast) isGate() {}

// admits says whether a reader is at or above the gate.
func admits(g ReaderGate, r ReaderVersion) bool {
	switch g := g.(type) {
	case NoGate:
		return true
	case AtLeast:
		return semver.Compare(readerSemver(r), g.V) >= 0
	}
	panic(fmt.Sprintf("config: %T is not a ReaderGate", g))
}

// readerSemver is the semver a reader is gated by: a release's own version,
// a development build's base, since a build between tags contains its base.
func readerSemver(r ReaderVersion) string {
	switch r := r.(type) {
	case Released:
		return r.V
	case Development:
		return r.Base
	}
	panic(fmt.Sprintf("config: %T is not a ReaderVersion", r))
}
