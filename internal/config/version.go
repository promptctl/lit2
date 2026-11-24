package config

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// ReaderVersion is the running binary's version, read once from Go's build
// stamp: a release such as v0.4.0, or a pseudo-version such as
// v0.4.1-0.20261112093000-abcdef123456 between tags, v0.0.0-… before the
// first. Semver orders a pseudo-version after the tag below it and before the
// next, so the stamp itself is what a gate compares. Modified says the tree
// held changes its commit does not. A build with no stamp is refused before
// any command runs, so no layer past main meets a reader it cannot place
// `[LAW:types-are-the-program]`.
type ReaderVersion struct {
	v        string
	Modified bool
}

// errNoVersion is a build that says no version it reads. Its refusal names
// buildVersioned, the build that would.
var errNoVersion = errors.New("a build with no version")

const buildVersioned = "build from a git checkout with `go build ./cmd/lit2`, or install a tagged version with `go install github.com/promptctl/lit2/cmd/lit2@<version>`"

// ReadReaderVersion parses the main module's version as Go stamps it. Go
// stamps `(devel)` when a build has no version, which lit2 refuses to run.
func ReadReaderVersion(stamp string) (ReaderVersion, error) {
	if stamp == "" || stamp == "(devel)" {
		return ReaderVersion{}, fmt.Errorf("%w: it was built without a version stamp, so it cannot say which workspace versions it reads; %s", errNoVersion, buildVersioned)
	}
	v, modified := strings.CutSuffix(stamp, "+dirty")
	if !semver.IsValid(v) {
		return ReaderVersion{}, fmt.Errorf("%w: the build stamp %q is not a version, so it cannot say which workspace versions it reads; %s", errNoVersion, stamp, buildVersioned)
	}
	return ReaderVersion{v: v, Modified: modified}, nil
}

// String is the reader's version as a person reads it.
func (r ReaderVersion) String() string {
	if r.Modified {
		return r.v + " (modified)"
	}
	return r.v
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
		return semver.Compare(r.v, g.V) >= 0
	}
	panic(fmt.Sprintf("config: %T is not a ReaderGate", g))
}
