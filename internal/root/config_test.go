package root

import (
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/assets"
	"github.com/promptctl/lit2/internal/git"
	"github.com/promptctl/lit2/internal/store"
	"github.com/promptctl/lit2/internal/verbs"
)

// freshSession is a session in a minted checkout before admin init.
func freshSession(t *testing.T) session {
	twin := NewFreshTwin()
	return session{t: t, twin: &twin}
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

// TestWithNoVersionEveryVerbIsRefusedNamingConfigCommit: nothing stands
// in for a version not applied; help says why it has no fields.
func TestWithNoVersionEveryVerbIsRefusedNamingConfigCommit(t *testing.T) {
	s := freshSession(t)
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
	if r.status != 0 || !strings.Contains(r.out, "workspace "+blob(def).String()+" applied as version one\n") || appends(r.event) != 1 {
		t.Fatalf("admin init: status %d, out %q, %d appends", r.status, r.out, appends(r.event))
	}
	if got := must(read(s.twin.Tree, verbs.WorkspaceFile, func(b []byte) ([]byte, error) { return b, nil })); string(got) != string(def) {
		t.Errorf("init wrote %q; want the default profile", got)
	}
	if got := s.ok("get", "/workspace"); !strings.Contains(got, blob(def).String()[:7]+" in force · .lit/workspace.toml matches") {
		t.Errorf("the board says %q", got)
	}
	if again := s.lit("admin", "init"); again.status != 0 || appends(again.event) != 0 || strings.Contains(again.out, "applied") {
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
	if r := bad.lit("get", "/"); reasonOf(r.event) != "workspace" {
		t.Errorf("after a refused init get / is %q; want no version", r.err)
	}
}

// TestConfigCommitAppliesOnlyWhatHEADHolds: absent, untracked and changed
// since HEAD are each refused naming the step; HEAD's file applies once.
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
	if got := freshSession(t).ok("config", "status"); got != "no version in force: lit2 config commit applies one\n" {
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

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
