package panics_test

import (
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/promptctl/lit2/internal/panics"
)

var errNamed = errors.New("the named precondition")

// recorder is a testing.TB that records whether a test failed.
type recorder struct {
	testing.TB
	failed bool
}

func (r *recorder) Helper()               {}
func (r *recorder) Errorf(string, ...any) { r.failed = true }

func TestAPanicIsTheNamedOneOnlyByIdentity(t *testing.T) {
	r := &recorder{TB: t}
	panics.With(r, errNamed, func() { panic(fmt.Errorf("pkg: %w: x", errNamed)) })
	if r.failed {
		t.Error("a panic wrapping the named error failed the test")
	}
	r = &recorder{TB: t}
	panics.With(r, errNamed, func() {})
	if !r.failed {
		t.Error("a call that did not panic passed")
	}
	another := errors.New("another")
	for name, c := range map[string]struct {
		f    func()
		same func(any) bool
	}{
		"another error": {func() { panic(another) }, func(r any) bool { return r == another }},
		"a string":      {func() { panic("the named precondition") }, func(r any) bool { return r == "the named precondition" }},
		"a nil dereference": {func() {
			var p *int
			_ = *p
		}, func(r any) bool { _, ok := r.(runtime.Error); return ok }},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); !c.same(r) {
					t.Errorf("%s came back as %v; want it panicked again as itself", name, r)
				}
			}()
			panics.With(&recorder{TB: t}, errNamed, c.f)
		})
	}
}
