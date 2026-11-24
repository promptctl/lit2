package root

import (
	"errors"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/command"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/verbs"
)

// freshSession is a session in a minted checkout before admin init.
func freshSession(t *testing.T) session {
	twin := NewFreshTwin()
	return session{t: t, twin: &twin}
}

// enrolledOnly is a session whose store holds the twin's enrollment, its
// founding, and no version: a store founded by a release that applied none.
func enrolledOnly(t *testing.T) session {
	s := freshSession(t)
	m := must(events.Mint(events.Context{Writer: twinID, Agent: events.Unlabeled{}, ConfigRef: blob(noPlan), Policy: 1, Now: s.twin.Process.Clock.Now()},
		events.NewDrafts(events.Act{About: events.Root{}, Payload: events.Enroll{Owner: verbs.Owner, Roles: verbs.DefaultRoles()}}), twinKey().Signer(), rand.NewChaCha8([32]byte{9})))
	must(s.twin.Process.Store.(command.Opened).Store.Append(m.Batch))
	s.pins(m.Events[0].ID())
	return s
}

// pins names e the founding of the session's clone.
func (s session) pins(e events.EventID) {
	s.t.Helper()
	shared, _ := s.twin.Process.IdentityFiles(git.Checkout{})
	if _, err := pin(shared, e); err != nil {
		s.t.Fatal(err)
	}
}

// file makes b the work tree's .lit/workspace.toml, or no file when b is
// nil.
func (s session) file(b []byte) {
	s.t.Helper()
	tree := store.NewMemoryFiles()
	if b != nil {
		if _, err := tree.CreateOnce(verbs.WorkspaceFile, b); err != nil {
			s.t.Fatal(err)
		}
	}
	s.twin.Process.WorkTree = func(git.Checkout) store.Files { return tree }
}

// commit commits b as the file in HEAD.
func (s session) commit(b []byte) {
	s.twin.Code.Commit(map[string][]byte{verbs.WorkspaceFile: b})
}

func blob(b []byte) git.ObjectID { return git.HashBlob(git.SHA1, b) }

// profile is a workspace of stories under features, its columns given.
func profile(columns string) []byte {
	return []byte("prefix = \"lit\"\nclaim_ttl = \"6h\"\n[levels.1]\nname = \"story\"\n[levels.2]\nname = \"feature\"\nhome = \"unfiled\"\n" + columns)
}

var (
	withPlan = profile("[columns.backlog]\ninitial = true\nnext = [\"plan\"]\n[columns.plan]\nnext = [\"done\"]\n[columns.done]\nterminal = true\n")
	noPlan   = profile("[columns.backlog]\ninitial = true\nnext = [\"done\"]\n[columns.done]\nterminal = true\n")
	aliased  = profile("[columns.backlog]\ninitial = true\nnext = [\"done\"]\n[columns.done]\nterminal = true\naliases = [\"plan\"]\n")
)

// TestTheConfigBlockNamesTheVersionInForce: a verb's event names the
// version it ran under by the blob id git gives its bytes.
func TestTheConfigBlockNamesTheVersionInForce(t *testing.T) {
	r := newSession(t).lit("get", "/")
	if want := blob(assets.DefaultWorkspace.Bytes()).String(); r.event.Config == nil || r.event.Config.Ref != want {
		t.Errorf("the config block is %+v; want the ref %s", r.event.Config, want)
	}
}

// TestWithNoEnrollmentEveryVerbIsRefusedNamingAdminInit: no policy says
// who may write a store no enrollment founded, so only init reads it.
func TestWithNoEnrollmentEveryVerbIsRefusedNamingAdminInit(t *testing.T) {
	s := freshSession(t)
	for _, args := range [][]string{{}, {"get", "/"}, {"add", "unfiled/children", "{title: a}"}, {"config", "commit"}, {"config", "status"}} {
		r := s.lit(args...)
		if r.status != 78 || reasonOf(r.event) != "policy" || !strings.Contains(r.err, "lit2 admin init") || appends(r.event) != 0 {
			t.Errorf("lit2 %q with no enrollment: status %d, stderr %q; want refused as the environment's, naming admin init", args, r.status, r.err)
		}
	}
	if help := s.ok("--help"); strings.Contains(help, "fields of ") || !strings.Contains(help, "fields: ") || !strings.Contains(help, "lit2 admin init") {
		t.Errorf("help with no enrollment:\n%s", help)
	}
}

// TestWithNoVersionEveryVerbIsRefusedNamingConfigCommit: nothing stands
// in for a version not applied; help says why it has no fields.
func TestWithNoVersionEveryVerbIsRefusedNamingConfigCommit(t *testing.T) {
	s := enrolledOnly(t)
	for _, args := range [][]string{{}, {"get", "/"}, {"add", "unfiled/children", "{title: a}"}} {
		r := s.lit(args...)
		if r.status != 78 || reasonOf(r.event) != "workspace" || !strings.Contains(r.err, "lit2 config commit") || r.event.Config != nil {
			t.Errorf("lit2 %q with no version: status %d, stderr %q, config %+v; want refused as the environment's, naming config commit", args, r.status, r.err, r.event.Config)
		}
	}
	if help := s.ok("--help"); strings.Contains(help, "fields of ") || !strings.Contains(help, "fields: ") || !strings.Contains(help, "lit2 config commit") {
		t.Errorf("help with no version:\n%s", help)
	}
}

// TestInitAppliesTheDefaultAsVersionOne: with no file, init writes the
// default profile into the work tree and applies it; a second init, the
// store holding events, mints only identity.
func TestInitAppliesTheDefaultAsVersionOne(t *testing.T) {
	s := freshSession(t)
	def := assets.DefaultWorkspace.Bytes()
	r := s.lit("admin", "init")
	enrolled := "enrolled: policy v1, owner " + twinKey().Principal().String() + "\n"
	if r.status != 0 || !strings.Contains(r.out, enrolled+"workspace "+blob(def).String()+" applied as version one\n") || appends(r.event) != 1 {
		t.Fatalf("admin init: status %d, out %q, %d appends; want the enrollment and version one in one frame", r.status, r.out, appends(r.event))
	}
	if got := must(read(s.twin.Tree, verbs.WorkspaceFile, func(b []byte) ([]byte, error) { return b, nil })); string(got) != string(def) {
		t.Errorf("init wrote %q; want the default profile", got)
	}
	if got := s.ok("get", "/workspace"); !strings.Contains(got, blob(def).String()[:7]+" in force · .lit/workspace.toml matches") {
		t.Errorf("the board says %q", got)
	}
	if p := r.event.Policy; p.Bootstrap != "enroll" || len(p.Verdicts) != 2 {
		t.Errorf("init's policy block is %+v; want it enrolling, both events judged", p)
	}
	if u, ok := r.event.Policy.Under.(fold.Enrolled); !ok || u.Policy.Version() != 1 {
		t.Errorf("init's policy block says it ended under %#v; want the version one it founded", r.event.Policy.Under)
	}
	for i, v := range r.event.Policy.Verdicts {
		if _, ok := v.Outcome.(fold.Applied); !ok {
			t.Errorf("init's event %d is %+v; want applied", i, v.Outcome)
		}
	}
	if again := s.lit("admin", "init"); again.status != 0 || appends(again.event) != 0 || strings.Contains(again.out, "applied") || strings.Contains(again.out, "enrolled") {
		t.Errorf("a second init: status %d, out %q, %d appends; want identity only", again.status, again.out, appends(again.event))
	}
}

// TestInitAppliesTheFileThereOrRefusesIt: a file in the work tree is
// version one, and one that does not parse applies nothing.
func TestInitAppliesTheFileThereOrRefusesIt(t *testing.T) {
	s := freshSession(t)
	s.file(noPlan)
	if out := s.ok("admin", "init"); !strings.Contains(out, "workspace "+blob(noPlan).String()+" applied") {
		t.Errorf("init over a file says %q", out)
	}
	bad := freshSession(t)
	bad.file([]byte("prefix = \n"))
	r := bad.lit("admin", "init")
	if r.status != 1 || reasonOf(r.event) != "workspace" || appends(r.event) != 0 {
		t.Errorf("init over a file that does not parse: status %d, stderr %q, %d appends; want refused, nothing applied", r.status, r.err, appends(r.event))
	}
	if r := bad.lit("get", "/"); reasonOf(r.event) != "policy" {
		t.Errorf("after a refused init get / is %q; want no enrollment, the founding written whole or not at all", r.err)
	}
}

// TestConfigCommitAppliesOnlyWhatHEADHolds: absent, untracked, not a file
// and changed since HEAD are each refused naming the step; HEAD's file
// applies once.
func TestConfigCommitAppliesOnlyWhatHEADHolds(t *testing.T) {
	s := freshSession(t)
	s.ok("admin", "init")
	next := append(assets.DefaultWorkspace.Bytes(), "# the next version\n"...)
	s.file(nil)
	if r := s.refused("workspace", "config", "commit"); !strings.Contains(r.err, "there is no .lit/workspace.toml") {
		t.Errorf("no file: %q", r.err)
	}
	s.file(next)
	if r := s.refused("workspace", "config", "commit"); !strings.Contains(r.err, "git add .lit/workspace.toml") {
		t.Errorf("a file HEAD does not hold: %q", r.err)
	}
	code := s.twin.Code
	s.twin.Process.Code = func(git.Checkout) git.Code { return symlinked{Code: code} }
	if r := s.refused("workspace", "config", "commit"); !strings.Contains(r.err, "HEAD holds .lit/workspace.toml as mode 120000, not a file") {
		t.Errorf("a path HEAD holds as a symlink: %q", r.err)
	}
	s.twin.Process.Code = func(git.Checkout) git.Code { return code }
	s.commit(next)
	s.file(append(next, "# edited\n"...))
	if r := s.refused("workspace", "config", "commit"); !strings.Contains(r.err, "git commit .lit/workspace.toml") {
		t.Errorf("a file changed since HEAD: %q", r.err)
	}
	s.file(next)
	if r := s.lit("config", "commit"); r.status != 0 || r.out != "applied "+blob(next).String()+"\n" || appends(r.event) != 1 {
		t.Fatalf("config commit: status %d, out %q, stderr %q", r.status, r.out, r.err)
	}
	if r := s.lit("get", "/"); r.event.Config.Ref != blob(next).String() {
		t.Errorf("after the commit a verb runs under %s; want %s", r.event.Config.Ref, blob(next))
	}
	if r := s.lit("config", "commit"); r.out != blob(next).String()+" is already in force\n" || appends(r.event) != 0 {
		t.Errorf("committing the version in force: %q, %d appends; want nothing written", r.out, appends(r.event))
	}
}

// TestAVersionThatStrandsTicketsIsRefusedWithTheAliasLine: a column still
// holding a story that the next version does not read is refused, printing
// the aliases line; with it, the version applies.
func TestAVersionThatStrandsTicketsIsRefusedWithTheAliasLine(t *testing.T) {
	s := freshSession(t)
	s.file(withPlan)
	s.ok("admin", "init")
	feature := filed(t, s.ok("add", "unfiled/children", "{title: f}"))
	story := filed(t, s.ok("add", feature+"/children", "{title: s}"))
	s.ok("set", story+"/status", "plan")
	s.file(noPlan)
	s.commit(noPlan)
	if r := s.refused("workspace", "config", "commit"); !strings.Contains(r.err, `aliases = ["plan"]`) {
		t.Errorf("a version stranding plan: %q; want the aliases line", r.err)
	}
	s.file(aliased)
	s.commit(aliased)
	if out := s.ok("config", "commit"); out != "applied "+blob(aliased).String()+"\n" {
		t.Errorf("the aliased version: %q", out)
	}
}

// TestConfigStatusSaysTheDriftAndTheHistory: the board's line, and
// whether the version in force was ever HEAD's file.
func TestConfigStatusSaysTheDriftAndTheHistory(t *testing.T) {
	s := newSession(t)
	def := blob(assets.DefaultWorkspace.Bytes()).String()
	if got := s.ok("config", "status"); got != def[:7]+" in force · .lit/workspace.toml matches\nin HEAD's history: yes\n" {
		t.Errorf("status of the twin:\n%s", got)
	}
	drafted := []byte("# a draft\n")
	s.file(drafted)
	if got := s.ok("config", "status"); !strings.Contains(got, "differs ("+blob(drafted).String()[:7]+")") {
		t.Errorf("status with a drafted file:\n%s", got)
	}
	other := freshSession(t)
	other.ok("admin", "init")
	if got := other.ok("config", "status"); !strings.Contains(got, "in HEAD's history: no") {
		t.Errorf("status of a version never committed:\n%s", got)
	}
	if got := enrolledOnly(t).ok("config", "status"); got != "no version in force: lit2 config commit applies one\n" {
		t.Errorf("status with no version: %q", got)
	}
}

// TestASecondReplicaRunsTheModelFromTheEventsAlone: a checkout holding
// the same events and no file runs the version they put in force.
func TestASecondReplicaRunsTheModelFromTheEventsAlone(t *testing.T) {
	s := freshSession(t)
	s.file(noPlan)
	s.ok("admin", "init")
	feature := filed(t, s.ok("add", "unfiled/children", "{title: f}"))
	other := NewFreshTwin()
	other.Process.Store = s.twin.Process.Store
	replica := session{t: t, twin: &other}
	replica.pins(must(foundingFileIn(s.twin.Process.WorkTree(git.Checkout{}))).(fold.FoundedBy).Enrollment)
	if got, want := replica.ok("get", feature), s.ok("get", feature); got != want {
		t.Errorf("the replica reads %s as\n%s\nwant\n%s", feature, got, want)
	}
	// the default's third level's home, which this version does not declare
	if r := replica.lit("get", "unassigned"); r.status == 0 {
		t.Errorf("the replica reads a home only the default declares: %q", r.out)
	}
	if got := replica.ok("get", "/workspace"); !strings.Contains(got, blob(noPlan).String()[:7]+" in force · .lit/workspace.toml is absent") {
		t.Errorf("the replica's board says %q", got)
	}
}

// TestAnIDUnderAFormerPrefixStillResolves: an id typed under a prefix any
// applied version declared resolves after the prefix changes, and one no
// version declared is another project's (§3.2).
func TestAnIDUnderAFormerPrefixStillResolves(t *testing.T) {
	s := freshSession(t)
	s.file(noPlan)
	s.ok("admin", "init")
	feature := filed(t, s.ok("add", "unfiled/children", "{title: f}"))
	renamed := []byte(strings.Replace(string(noPlan), `prefix = "lit"`, `prefix = "zzz"`, 1))
	s.file(renamed)
	s.commit(renamed)
	s.ok("config", "commit")
	seg := strings.TrimPrefix(feature, "lit-")
	for _, typed := range []string{feature, "zzz-" + seg, seg} {
		if got := s.ok("get", typed+"/title"); got != "f\n" {
			t.Errorf("get %s/title = %q; want f", typed, got)
		}
	}
	if got := s.ok("get", "unfiled/children"); !strings.Contains(got, "zzz-"+seg) {
		t.Errorf("the feature renders as %q; want it under zzz", got)
	}
	if r := s.lit("get", "qqq-"+seg+"/title"); r.status == 0 || !strings.Contains(r.err, "another project's") {
		t.Errorf("an id under a prefix no version declared: status %d, %q", r.status, r.err)
	}
}

// TestInitAppliesVersionOneToAStoreWithEventsAndNoVersion: a store whose
// events predate any apply runs nothing until init applies version one.
func TestInitAppliesVersionOneToAStoreWithEventsAndNoVersion(t *testing.T) {
	s := freshSession(t)
	three(t, *s.twin, 1)
	def := assets.DefaultWorkspace.Bytes()
	if r := s.lit("admin", "init"); r.status != 0 || !strings.Contains(r.out, "workspace "+blob(def).String()+" applied as version one\n") || appends(r.event) != 1 {
		t.Fatalf("admin init over events and no version: status %d, out %q, stderr %q", r.status, r.out, r.err)
	}
	if r := s.lit("get", "/"); r.status != 0 {
		t.Errorf("after init get / is %d, %q; want it run under version one", r.status, r.err)
	}
}

// TestConfigCommitRecordsTheVersionItRanUnder: the run that applies a
// later version names the one in force when it wrote.
func TestConfigCommitRecordsTheVersionItRanUnder(t *testing.T) {
	s := newSession(t)
	next := append(assets.DefaultWorkspace.Bytes(), "# the next version\n"...)
	s.file(next)
	s.commit(next)
	if r := s.lit("config", "commit"); r.status != 0 || r.event.Config == nil || r.event.Config.Ref != blob(assets.DefaultWorkspace.Bytes()).String() {
		t.Errorf("config commit: status %d, config %+v; want the default's ref", r.status, r.event.Config)
	}
}

// TestAVersionInForceThisReleaseCannotReadRefusesConfigCommit: the
// environment's, naming the release, never a lit2 bug.
func TestAVersionInForceThisReleaseCannotReadRefusesConfigCommit(t *testing.T) {
	s := freshSession(t)
	s.file(append([]byte("min-reader-version = \"0.4.0\"\n"), noPlan...))
	s.ok("admin", "init")
	older, err := config.ReadReaderVersion("v0.3.0")
	if err != nil {
		t.Fatal(err)
	}
	s.twin.Process.Reader = older
	s.file(noPlan)
	s.commit(noPlan)
	if r := s.lit("config", "commit"); r.status != 78 || reasonOf(r.event) != "workspace" || !strings.Contains(r.err, "does not read under this release") || appends(r.event) != 0 {
		t.Errorf("config commit under an unreadable version: status %d, stderr %q, %d appends", r.status, r.err, appends(r.event))
	}
}

// symlinked is a code repository whose HEAD holds every path as a symlink.
type symlinked struct{ git.Code }

func (symlinked) AtHead(string) (git.HeadBlob, error) { return git.NotAFile{Mode: "120000"}, nil }

// unreadableHistory is a code repository whose history does not read.
type unreadableHistory struct{ git.Code }

func (unreadableHistory) EverAt(string, git.ObjectID) (bool, error) {
	return false, errors.New("the history is gone")
}

// TestConfigStatusSaysTheLineWhenTheHistoryDoesNotRead: only the history
// half is refused.
func TestConfigStatusSaysTheLineWhenTheHistoryDoesNotRead(t *testing.T) {
	s := newSession(t)
	code := s.twin.Code
	s.twin.Process.Code = func(git.Checkout) git.Code { return unreadableHistory{Code: code} }
	r := s.lit("config", "status")
	def := blob(assets.DefaultWorkspace.Bytes()).String()
	if r.status != 78 || reasonOf(r.event) != "workspace" || r.out != def[:7]+" in force · .lit/workspace.toml matches\n" || !strings.Contains(r.err, "the history is gone") {
		t.Errorf("config status with no history: status %d, out %q, stderr %q", r.status, r.out, r.err)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// TestConfigCommitByAContributorIsRefusedNamingSetWorkspace: applying a
// version writes the root, which only an owner may; the refusal says so
// and nothing is written.
func TestConfigCommitByAContributorIsRefusedNamingSetWorkspace(t *testing.T) {
	s := newSession(t)
	beside := NewTwinBeside(*s.twin)
	joiner := session{t: t, twin: &beside}
	joiner.ok("admin", "init")
	s.ok("add", "/members", "{principal: \""+besideKey().Principal().String()+"\", role: contributor}")
	next := append(assets.DefaultWorkspace.Bytes(), "# the next version\n"...)
	joiner.file(next)
	joiner.commit(next)
	r := joiner.lit("config", "commit")
	if r.status == 0 || !strings.Contains(r.err, "set /workspace refused: role contributor") || appends(r.event) != 0 {
		t.Errorf("a contributor's config commit: status %d, stderr %q, %d appends; want refused naming set /workspace", r.status, r.err, appends(r.event))
	}
	if v := r.event.Policy.Verdicts; len(v) != 1 {
		t.Errorf("the refused commit's verdicts are %+v; want its one event", v)
	}
}
