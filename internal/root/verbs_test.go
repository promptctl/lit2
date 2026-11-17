package root

import (
	"fmt"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/store"
)

// session is one twin run many times, as a person in one checkout does.
type session struct {
	t    *testing.T
	twin *Twin
}

func newSession(t *testing.T) session {
	twin := NewTwin()
	return session{t: t, twin: &twin}
}

// said is one invocation's status, stdout, stderr and event.
type said struct {
	status   int
	out, err string
	event    command.Event
}

// lit runs one invocation with stdin empty.
func (s session) lit(args ...string) said { return s.piped("", args...) }

// piped runs one invocation with stdin holding in.
func (s session) piped(in string, args ...string) said {
	s.t.Helper()
	s.twin.Out.Reset()
	s.twin.Err.Reset()
	s.twin.Process.In = strings.NewReader(in)
	n := len(s.twin.Commands.Events)
	status := Run(s.twin.Process, args)
	if len(s.twin.Commands.Events) != n+1 {
		s.t.Fatalf("lit2 %q left %d events; want 1", args, len(s.twin.Commands.Events)-n)
	}
	return said{status: status, out: s.twin.Out.String(), err: s.twin.Err.String(), event: s.twin.Commands.Events[n]}
}

// ok runs an invocation that must succeed, and is its stdout.
func (s session) ok(args ...string) string {
	s.t.Helper()
	r := s.lit(args...)
	if r.status != 0 {
		s.t.Fatalf("lit2 %q: status %d, stderr %q", args, r.status, r.err)
	}
	return r.out
}

// refused runs an invocation that must be refused for reason, and is what
// it said.
func (s session) refused(reason string, args ...string) said {
	s.t.Helper()
	r := s.lit(args...)
	if r.status != 1 || reasonOf(r.event) != reason {
		s.t.Fatalf("lit2 %q: status %d, stderr %q; want refused as %s", args, r.status, r.err, reason)
	}
	if r.out != "" {
		s.t.Errorf("lit2 %q was refused and printed %q", args, r.out)
	}
	return r
}

// filed is the id a create's echo names on its first line, "+ <id>".
func filed(t *testing.T, echo string) string {
	t.Helper()
	first, _, _ := strings.Cut(echo, "\n")
	id, ok := strings.CutPrefix(first, "+ ")
	if !ok {
		t.Fatalf("the echo %q names no new item first", echo)
	}
	return id
}

func appends(e command.Event) int { return len(e.Store.Appends) }

// TestAPersonFilesAndWorksATicketByPath is the ticket's done-when: file a
// ticket, read it, retitle it, comment on it and edit the comment, label
// it, ask whether ready holds, remove it and restore it.
func TestAPersonFilesAndWorksATicketByPath(t *testing.T) {
	s := newSession(t)
	echo := s.ok("add", "unfiled/children", `{title: "History reads come from one store"}`)
	id := filed(t, echo)
	want := "+ " + id + "\n⇒ " + id + "/parent = unfiled\n⇒ " + id + "/title = \"History reads come from one store\"\n"
	if echo != want {
		t.Errorf("the create echoes\n%s\nwant\n%s", echo, want)
	}
	if got := s.ok("get", id+"/title"); got != "History reads come from one store\n" {
		t.Errorf("get title = %q", got)
	}
	item := s.ok("get", id)
	for _, line := range []string{"$ = " + id + "\n", "$/title\tHistory reads come from one store\n", "$/parent\tunfiled\n", "$/level\t2\n", "$/ready\tunmet description (ready)\n"} {
		if !strings.Contains(item, line) {
			t.Errorf("get %s lacks %q:\n%s", id, line, item)
		}
	}
	if got := s.ok("set", id+"/title", "One store for history"); got != id+"/title \"History reads come from one store\" → \"One store for history\"\n" {
		t.Errorf("set title echoes %q", got)
	}
	comment := s.ok("add", id+"/comments", "the read path is in fold")
	seg, ok := strings.CutPrefix(strings.TrimSuffix(comment, "\n"), "+ "+id+"/comments/")
	if !ok {
		t.Fatalf("add comment echoes %q", comment)
	}
	note := id + "/comments/" + seg
	if got := s.ok("get", note); got != "$ = "+note+"\n$/body\tthe read path is in fold\n$/by\t"+twinKey().Principal().String()+"\n" {
		t.Errorf("get the comment = %q; want its fields, its writer among them", got)
	}
	if got := s.ok("set", note+"/body", "the read path is in the fold"); got != note+"/body \"the read path is in fold\" → \"the read path is in the fold\"\n" {
		t.Errorf("editing the comment echoes %q", got)
	}
	if got := s.ok("get", note+"/body"); got != "the read path is in the fold\n" {
		t.Errorf("the edited comment's body = %q; want the edit alone", got)
	}
	if got := s.ok("rm", note); got != note+"/deleted (unset) → true\n" {
		t.Errorf("rm the comment echoes %q", got)
	}
	if got := s.ok("get", note+"/deleted"); got != "true\n" {
		t.Errorf("a removed comment's deleted = %q", got)
	}
	if got := s.ok("rm", note); got != "" {
		t.Errorf("a second rm of the comment echoes %q; want nothing drafted", got)
	}
	if got := s.ok("get", id+"/comments"); !strings.HasPrefix(got, "(deleted) [") || !strings.HasSuffix(got, "] the read path is in the fold\n") {
		t.Errorf("a removed comment lists as %q; want it listed, marked", got)
	}
	if got := s.ok("set", note+"/deleted", "false"); got != note+"/deleted true → false\n" {
		t.Errorf("restoring the comment echoes %q", got)
	}
	if got := s.ok("add", id+"/labels", "ready"); got != "+ "+id+"/labels/ready\n" {
		t.Errorf("add label echoes %q", got)
	}
	if got := s.ok("get", id+"/ready"); got != "unmet description (ready)\n" {
		t.Errorf("get ready = %q; want the description test unmet", got)
	}
	if got := s.ok("rm", id); got != id+"/deleted (unset) → true\n" {
		t.Errorf("rm echoes %q", got)
	}
	if got := s.ok("get", id+"/deleted"); got != "true\n" {
		t.Errorf("a removed item's deleted = %q", got)
	}
	if got := s.ok("set", id+"/deleted", "false"); got != id+"/deleted true → false\n" {
		t.Errorf("restore echoes %q", got)
	}
}

// TestADryRunWritesNothingAndEchoesWhatARunMints: --dry-run appends
// nothing, and its echo is the real run's with each new node unnamed.
func TestADryRunWritesNothingAndEchoesWhatARunMints(t *testing.T) {
	s := newSession(t)
	dry := s.lit("add", "--dry-run", "unfiled/children", `[{title: a}, {title: b}]`)
	if dry.status != 0 || appends(dry.event) != 0 || !dry.event.Verbs.DryRun {
		t.Fatalf("a dry run: status %d, appends %d, verbs %+v", dry.status, appends(dry.event), dry.event.Verbs)
	}
	real := s.lit("add", "unfiled/children", `[{title: a}, {title: b}]`)
	if appends(real.event) != 1 {
		t.Fatalf("the real run appended %d frames; want one", appends(real.event))
	}
	a, b := filed(t, real.out), filed(t, strings.SplitN(real.out, "\n", 4)[3])
	named := strings.NewReplacer(a+"/", "(new 1)/", "+ "+a, "+ (new 1)", b+"/", "(new 2)/", "+ "+b, "+ (new 2)").Replace(real.out)
	if dry.out != named+"(dry run: nothing written)\n" {
		t.Errorf("the dry run echoed\n%s\nthe run\n%s", dry.out, real.out)
	}
	if noop := s.lit("set", "--dry-run", a+"/title", "a"); noop.out != "(dry run: nothing written)\n" {
		t.Errorf("a dry run that changes nothing echoed %q", noop.out)
	}
}

// TestAWriteThatChangesNothingMintsNothing: rm twice, a title set to
// itself, a held label, an unwritten status set to its initial column;
// none drafts, each exits 0 and records drafts=0.
func TestAWriteThatChangesNothingMintsNothing(t *testing.T) {
	s := newSession(t)
	id := filed(t, s.ok("add", "unfiled/children", "{title: a, labels: [ready]}"))
	s.ok("rm", id)
	story := filed(t, s.ok("add", id+"/children", "{title: s}"))
	for _, args := range [][]string{
		{"rm", id},
		{"set", id + "/deleted", "true"},
		{"set", id + "/title", "a"},
		{"add", id + "/labels", "ready"},
		{"set", story + "/status", "backlog"},
		{"rm", id + "/labels/external"},
	} {
		r := s.lit(args...)
		if r.status != 0 || r.out != "" || appends(r.event) != 0 || r.event.Verbs.Drafts != 0 {
			t.Errorf("lit2 %q: status %d, out %q, appends %d, verbs %+v; want nothing drafted", args, r.status, r.out, appends(r.event), r.event.Verbs)
		}
	}
	if got := s.ok("add", "/children", "{level: 1, title: s, status: backlog}"); strings.Contains(got, "status") {
		t.Errorf("a new story given its initial column writes it: %q", got)
	}
}

// TestACommandWritesEachTargetOnce: a register, a member and standard
// input are each written once in a command; a key is given once in an
// object; and new things, children and comments, claim nothing.
func TestACommandWritesEachTargetOnce(t *testing.T) {
	s := newSession(t)
	id := filed(t, s.ok("add", "unfiled/children", "{title: a}"))
	s.refused("twice", "set", id+"/title", "b", id+"/title", "c")
	s.refused("twice", "add", id+"/labels", "[ready, ready]")
	s.refused("twice", "add", id+"/labels", "ready", id+"/labels", "[external, ready]")
	r := s.piped("x\n", "set", id+"/description", "-", id+"/reason", "-")
	if r.status != 1 || reasonOf(r.event) != "twice" {
		t.Errorf("two reads of stdin: status %d, stderr %q", r.status, r.err)
	}
	s.refused("value", "add", "unfiled/children", "{title: a, title: b}")
	s.refused("value", "add", "/children", "{level: 1, level: 2, title: a}")
	three := s.ok("add", "unfiled/children", "[{title: a}, {title: a}, {title: a}]")
	if strings.Count(three, "\n+ ") != 2 || !strings.HasPrefix(three, "+ ") {
		t.Errorf("three children echo %q; want three creates", three)
	}
	two := s.ok("add", id+"/comments", "one", id+"/comments", "two")
	if strings.Count(two, "+ "+id+"/comments/") != 2 {
		t.Errorf("two comments echo %q", two)
	}
	note := strings.TrimPrefix(strings.SplitN(two, "\n", 2)[0], "+ ")
	s.refused("twice", "rm", note, note)
	s.refused("not_removable", "rm", id+"/comments")
}

// TestAWriteRefusedAnywhereWritesNothingAndRecordsEveryOperand: one bad
// operand refuses the command whole; every operand is on the event, and a
// value's text is not.
func TestAWriteRefusedAnywhereWritesNothingAndRecordsEveryOperand(t *testing.T) {
	s := newSession(t)
	id := filed(t, s.ok("add", "unfiled/children", "{title: a}"))
	r := s.refused("unknown_field", "set", id+"/title", "secret words", id+"/nope", "more secrets")
	if appends(r.event) != 0 {
		t.Errorf("a refused command appended %d frames", appends(r.event))
	}
	ops := r.event.Verbs.Operands
	if len(ops) != 2 || ops[0].Canon != id+"/title" || ops[0].Values != 1 || ops[1].Reason != "unknown_field" || ops[1].Detail == "" {
		t.Errorf("the operands recorded %+v", ops)
	}
	if got := s.ok("get", id+"/title"); got != "a\n" {
		t.Errorf("a refused command moved the title to %q", got)
	}
	if got := fmt.Sprintf("%+v %+v", r.event.Command, r.event.Verbs); strings.Contains(got, "secret") {
		t.Errorf("a value's text reached the command's record: %s", got)
	}
	s.refused("value", "set", id+"/title", "")
	s.refused("value", "add", id+"/comments", "")
	s.refused("value", "add", "unfiled/children", "{description: d}")
	s.refused("value", "add", id+"/labels", "nope")
}

// TestPlacementIsTheNodesAddedTo: under / a child goes to its level's
// home, the top level by default; under a home at its level; under an
// item one level below; and level is placement only under /.
func TestPlacementIsTheNodesAddedTo(t *testing.T) {
	s := newSession(t)
	feature := s.ok("add", "/children", `{level: 2, title:"History reads come from one store"}`)
	m2q := filed(t, feature)
	if !strings.Contains(feature, "⇒ "+m2q+"/parent = unfiled\n") {
		t.Errorf("a level-2 child of / echoes %q; want it in unfiled", feature)
	}
	if got := s.ok("get", m2q+"/level"); got != "2\n" {
		t.Errorf("its level = %q", got)
	}
	epic := filed(t, s.ok("add", "/children", "{title: e}"))
	if got := s.ok("get", epic+"/parent"); got != "unassigned\n" {
		t.Errorf("a child of / with no level is at the top level, in unassigned: parent %q", got)
	}
	s.refused("derived", "add", "unfiled/children", "{level: 3, title: x}")
	stories := s.ok("add", m2q+"/children", "[{title: s1}, {title: s2}, {title: s3}]")
	s1 := filed(t, stories)
	if got := s.ok("get", s1+"/status"); got != "backlog\n" {
		t.Errorf("a new story's status = %q; want backlog", got)
	}
	if got := s.ok("get", s1+"/level"); got != "1\n" {
		t.Errorf("a story's level = %q", got)
	}
	s.refused("not_on_level", "add", s1+"/children", "{title: x}")
	s.refused("not_on_level", "get", m2q+"/status")
	// scenarios.md §2 step 5: a label on the feature grooms its stories
	s.ok("add", m2q+"/labels", "ready")
	if got := s.ok("get", s1+"/ready"); got != "unmet description (ready)\n" {
		t.Errorf("a story under a ready feature reads %q; want only description unmet", got)
	}
	s.ok("set", s1+"/description", "what and why")
	if got := s.ok("get", s1+"/ready"); got != "holds\n" {
		t.Errorf("a described story under a ready feature reads %q; want holds", got)
	}
	s.ok("rm", m2q)
	if got := s.ok("get", m2q+"/children"); strings.Count(got, "\n") != 3 {
		t.Errorf("a deleted feature's children = %q; want its three stories", got)
	}
	if got := s.ok("get", "/children"); got != "/children\tunassigned\n/children\tunfiled\n" && got != "unassigned\nunfiled\n" {
		t.Errorf("the root's children = %q; want its homes", got)
	}
	s.refused("not_removable", "rm", "unfiled")
	s.refused("not_removable", "rm", "/")
}

// TestStdinIsOneValueVerbatim: standard input is one value, never read by
// the literal grammar, less one final newline.
func TestStdinIsOneValueVerbatim(t *testing.T) {
	s := newSession(t)
	id := filed(t, s.ok("add", "unfiled/children", "{title: a}"))
	if r := s.piped("[a, b]\n", "set", id+"/description", "-"); r.status != 0 {
		t.Fatalf("stdin like a list: %q", r.err)
	}
	if got := s.ok("get", id+"/description"); got != "[a, b]\n" {
		t.Errorf("description = %q; want the text as given", got)
	}
}

// TestHelpAndAnUnknownFieldListTheSameRows: the refusal names what help
// prints at the place.
func TestHelpAndAnUnknownFieldListTheSameRows(t *testing.T) {
	s := newSession(t)
	id := filed(t, s.ok("add", "unfiled/children", "{title: a}"))
	r := s.refused("unknown_field", "get", id+"/nope")
	help := s.ok("--help")
	section := help[strings.Index(help, "fields of an item at level 2:"):]
	section = section[:strings.Index(section[1:], "\nfields of ")+1]
	for _, l := range strings.Split(section, "\n")[1:] {
		f := strings.Fields(l)
		if len(f) > 0 && !strings.Contains(r.err, f[0]) {
			t.Errorf("help lists %s at level 2 and the refusal %q does not", f[0], r.err)
		}
	}
	if !strings.Contains(r.err, "title") {
		t.Errorf("the refusal %q lists no rows", r.err)
	}
}

// TestInitPrintsThePrincipal: admin init in a minted checkout finds its
// key and prints its principal, and no phrase, and its event says it found
// both files.
func TestInitPrintsThePrincipal(t *testing.T) {
	s := newSession(t)
	r := s.lit("admin", "init")
	if r.out != "principal "+twinKey().Principal().String()+"\n" {
		t.Errorf("admin init printed %q", r.out)
	}
	if got := r.event.Store.Inits; len(got) != 1 || got[0] != (command.Fresh{Checkout: store.Existing, Key: store.Existing}) {
		t.Errorf("admin init's event records %+v; want both files found", got)
	}
	if again := s.ok("admin", "init"); again != r.out {
		t.Errorf("a second init printed %q; want %q", again, r.out)
	}
}

// TestAnItemReadsItsFieldsThenItsComments, and a member its kind refuses
// is a typo, refused as rm refuses it.
func TestAnItemReadsItsFieldsThenItsComments(t *testing.T) {
	s := newSession(t)
	id := filed(t, s.ok("add", "unfiled/children", `{title: a}`))
	s.ok("add", id+"/comments", "a note")
	item := s.ok("get", id)
	if c, ti := strings.Index(item, "$/comments/"), strings.Index(item, "$/title\t"); c < ti {
		t.Errorf("get %s prints a comment before a field:\n%s", id, item)
	}
	s.refused("value", "get", id+"/labels/reday")
	if got := s.ok("get", id+"/labels/ready"); got != "" {
		t.Errorf("a declared label not held reads %q", got)
	}
}

// TestABareLit2IsTheBoardOfAnEmptyWorkspace: a fresh workspace reads its
// homes, no lanes, and nothing next out of none considered, and bare lit2
// is that read (scenarios.md 1).
func TestABareLit2IsTheBoardOfAnEmptyWorkspace(t *testing.T) {
	s := newSession(t)
	want := "$ = /\n$/children\tunassigned · home\n$/children\tunfiled · home\n$/lanes\t(none)\n$/next\t(none)\n$/next\tlanes considered: 0\n"
	if got := s.ok(); got != want {
		t.Errorf("a bare lit2 prints\n%s\nwant\n%s", got, want)
	}
	if r := s.lit(); r.event.Command.Name != "get" || len(r.event.Command.Paths) != 1 || r.event.Command.Paths[0] != "/" {
		t.Errorf("a bare lit2 records %+v; want a get of /", r.event.Command)
	}
}

// TestTheBoardIsTheTreeTheLanesAndNext: get / shows every node with its
// stage, each leaf its own lane in the order it was filed, and next the
// first lane's head; once that head is done, next serves the second
// (scenarios.md 2 and 14, at slice 1's depth).
func TestTheBoardIsTheTreeTheLanesAndNext(t *testing.T) {
	s := newSession(t)
	feature := filed(t, s.ok("add", "unfiled/children", "{title: board}"))
	first := filed(t, s.ok("add", feature+"/children", "{title: tree}"))
	second := filed(t, s.ok("add", feature+"/children", "{title: lanes}"))
	want := "$ = /\n" +
		"$/children\tunassigned · home\n" +
		"$/children\tunfiled · home\n" +
		"$/children\t" + feature + " · initial · backlog 2 · board\n" +
		"$/children\t" + first + " · backlog · tree\n" +
		"$/children\t" + second + " · backlog · lanes\n" +
		"$/lanes\t" + first + " · backlog · tree\n" +
		"$/lanes\t" + second + " · backlog · lanes\n" +
		"$/next\t" + first + " · backlog · tree\n" +
		"$/next\tlanes considered: 1\n"
	if got := s.ok("get", "/"); got != want {
		t.Errorf("get / prints\n%s\nwant\n%s", got, want)
	}
	s.ok("set", first+"/status", "obsolete", first+"/reason", "superseded by the board")
	want = "/next\t" + second + " · backlog · lanes\n/next\tlanes considered: 2\n"
	if got := s.ok("get", "/next"); got != want {
		t.Errorf("once the first is closed get /next prints\n%s\nwant\n%s", got, want)
	}
}

// TestTheBoardsRowsAreDerived: lanes and next are the engine's, so a
// write to either is refused as derived and nothing is written.
func TestTheBoardsRowsAreDerived(t *testing.T) {
	s := newSession(t)
	for _, p := range []string{"/lanes", "/next"} {
		if r := s.refused("derived", "set", p, "x"); appends(r.event) != 0 {
			t.Errorf("set %s x appended %d", p, appends(r.event))
		}
	}
}

// TestAMoveTheGateRefusesWritesNothing: a dry run asks the gate as a run
// does, and a refused move appends nothing and records the gate's reason;
// a move whose requirements the same command meets lands.
func TestAMoveTheGateRefusesWritesNothing(t *testing.T) {
	s := newSession(t)
	feature := filed(t, s.ok("add", "unfiled/children", "{title: f, labels: [ready]}"))
	story := filed(t, s.ok("add", feature+"/children", "{title: s}"))
	for _, args := range [][]string{{"set", "--dry-run", story + "/status", "plan"}, {"set", story + "/status", "plan"}} {
		r := s.refused("gate", args...)
		if appends(r.event) != 0 || !strings.Contains(r.err, "description (ready)  found (none)") {
			t.Errorf("lit2 %q: appends %d, stderr %q", args, appends(r.event), r.err)
		}
		if got := r.event.Verbs.Requirements; got.Evaluated != 5 || got.Errors != 1 {
			t.Errorf("lit2 %q records requirements %+v; want 5 evaluated, 1 unmet error", args, got)
		}
	}
	r := s.lit("set", story+"/description", "what", story+"/status", "plan")
	if r.status != 0 || !strings.Contains(r.out, story+"/status backlog → plan\n") {
		t.Errorf("a move groomed in its own command: status %d, echo %q", r.status, r.out)
	}
	if got := r.event.Verbs.Requirements; got != (command.RequirementCounts{Evaluated: 5}) {
		t.Errorf("the landed move records requirements %+v; want 5 evaluated, none unmet", got)
	}
}
