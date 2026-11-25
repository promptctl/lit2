package root

import (
	"errors"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/files"
	"github.com/promptctl/lit2/internal/files/filestest"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
)

// founding is what the session's clone pins and what its work tree's
// FoundingFile names.
func (s session) founding() (pin, named fold.Founding) {
	s.t.Helper()
	return must(pinned(s.twin.Process.Pin(git.Checkout{}))), must(foundingFileIn(s.twin.Process.WorkTree(git.Checkout{})))
}

func TestInitFoundsTheStoreAndNamesItsFoundingToCommit(t *testing.T) {
	s := freshSession(t)
	r := s.lit("admin", "init")
	pin, named := s.founding()
	f, founded := pin.(fold.FoundedBy)
	if r.status != 0 || !founded || named != pin {
		t.Fatalf("admin init: status %d, stderr %q, pin %#v, %s %#v", r.status, r.err, pin, FoundingFile(), named)
	}
	if want := "founding " + f.Enrollment.String() + " named in .lit/founding: commit it"; !strings.Contains(r.out, want) {
		t.Errorf("admin init printed %q; want it to say %q", r.out, want)
	}
	if r.event.Policy.Verdicts[0].Event != f.Enrollment || r.event.Policy.Founding != pin {
		t.Errorf("the pin names %s; the init wrote %s and its event says it pinned %#v", f.Enrollment, r.event.Policy.Verdicts[0].Event, r.event.Policy.Founding)
	}
	if again := s.ok("admin", "init"); strings.Contains(again, "founding") {
		t.Errorf("a second init printed %q; want the store it founded kept", again)
	}
}

// TestInitFoundsOverAnEnrollmentNoFileNames: a store holding another
// writer's enrollment is not adopted for being there first; a clone adopts
// only the founding its committed file names.
func TestInitFoundsOverAnEnrollmentNoFileNames(t *testing.T) {
	owner := NewFreshTwin()
	usurper := NewTwinBeside(owner)
	session{t: t, twin: &usurper}.ok("admin", "init")
	s := session{t: t, twin: &owner}
	if out := s.ok("admin", "init"); !strings.Contains(out, "enrolled: policy v1, owner "+twinKey().Principal().String()) {
		t.Fatalf("the owner's init printed %q; want it to found the store", out)
	}
	if got := s.ok("get", "/members"); strings.Contains(got, besideKey().Principal().String()) {
		t.Errorf("the members are\n%s\nwant the usurper's enrollment founding nothing", got)
	}
}

func TestInitRefusesAFoundingFileNamingAnotherFounding(t *testing.T) {
	s := newSession(t)
	tree := files.NewMemory()
	filestest.Create(t, tree, FoundingFile(), events.EventID{1}.FileText())
	s.twin.Process.WorkTree = func(git.Checkout) files.Files { return tree }
	r := s.lit("admin", "init")
	var contradicted foundingContradicted
	if r.status == 0 || !errors.As(failure(t, r.event), &contradicted) || !strings.Contains(r.err, "the pin stands") || !strings.Contains(r.err, pinPath(twinCheckout)) {
		t.Errorf("init over a file naming another founding: status %d, stderr %q", r.status, r.err)
	}
	if pin, _ := s.founding(); pin == fold.Founding(fold.FoundedBy{Enrollment: events.EventID{1}}) {
		t.Error("the file moved the pin")
	}
}
