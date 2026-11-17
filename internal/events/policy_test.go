package events

import (
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/events/wire"
)

func TestACapabilityReadsBackAsItsText(t *testing.T) {
	for _, text := range []string{
		"add */children", "add own/*", "set own/*", "set */status", "add */comments", "rm own/* (level=1)",
		"set */*", "add */*", "rm */*/*", "set assignee/pr/verdict", "rm own-or-assignee/* (level!=2 level=1)",
	} {
		c, err := ParseCapability(text)
		if err != nil {
			t.Fatalf("%s: %v", text, err)
		}
		if c.String() != text {
			t.Errorf("%s reads back as %s", text, c)
		}
	}
}

func TestATextThatIsNoCapabilityIsRefusedSayingWhy(t *testing.T) {
	for text, why := range map[string]string{
		"get */*":            "verb is set, add or rm",
		"set /policy":        "subject is",
		"set own":            "a subject, then / and a field glob",
		"set own/":           "a subject, then / and a field glob",
		"set own/a//b":       "empty step",
		"rm own/* (level=9)": "1 to 5",
		"rm own/* (stage=x)": "level=N",
		"rm own/* (level=1":  "in parentheses",
	} {
		_, err := ParseCapability(text)
		if err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%s: %v, want a refusal saying %q", text, err, why)
		}
	}
}

func TestAFinalStarIsTheSubtreeAndAnEarlierOneOneSegment(t *testing.T) {
	for _, c := range []struct {
		glob  string
		field []string
		in    bool
	}{
		{"set own/*", nil, true},
		{"set own/*", []string{"labels", "urgent"}, true},
		{"rm */*/*", nil, false},
		{"rm */*/*", []string{"labels"}, true},
		{"rm */*/*", []string{"labels", "urgent"}, true},
		{"set */status", []string{"status"}, true},
		{"set */status", []string{"title"}, false},
		{"set */status", []string{"status", "x"}, false},
		{"set */status", nil, false},
	} {
		if got := MustCapability(c.glob).Field.Matches(c.field); got != c.in {
			t.Errorf("%s matching %v is %v, want %v", c.glob, c.field, got, c.in)
		}
	}
}

func TestAPolicyThatLocksItsWorkspaceOutIsNoPolicy(t *testing.T) {
	owner, contributor := MustName("owner"), MustName("contributor")
	a, b := keyOf(1).Principal(), keyOf(2).Principal()
	roles := map[Name][]Capability{contributor: {MustCapability("set own/*")}}
	for why, try := range map[string]func() error{
		"no member holds the owner role": func() error {
			_, err := NewPolicy(1, owner, roles, map[PrincipalID]Name{a: contributor}, Majority{})
			return err
		},
		"not a role this policy declares": func() error {
			_, err := NewPolicy(1, owner, roles, map[PrincipalID]Name{a: owner, b: MustName("editor")}, Majority{})
			return err
		},
		"is the owner role": func() error {
			_, err := NewPolicy(1, owner, map[Name][]Capability{owner: nil}, map[PrincipalID]Name{a: owner}, Majority{})
			return err
		},
		"more than the 1 owners": func() error {
			_, err := NewPolicy(1, owner, roles, map[PrincipalID]Name{a: owner, b: contributor}, Fixed{N: 2})
			return err
		},
	} {
		if err := try(); err == nil || !strings.Contains(err.Error(), why) {
			t.Errorf("%v, want a refusal saying %q", err, why)
		}
	}
	p, err := NewPolicy(1, owner, roles, map[PrincipalID]Name{a: owner, b: contributor}, Fixed{N: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Owners(); len(got) != 1 || got[0] != a {
		t.Errorf("owners are %v, want only %s", got, a)
	}
	if r, ok := p.Role(b); !ok || r != contributor {
		t.Errorf("%s's role is %s %v, want contributor", b, r, ok)
	}
}

func readBack[P Payload](t *testing.T, p P, read func(*wire.Reader) (Versioned, error)) P {
	t.Helper()
	got, err := read(wire.NewReader(payloadBytes(p)))
	if err != nil {
		t.Fatalf("%s: %v", p.Kind(), err)
	}
	return got.(P)
}

func TestThePolicyPayloadsReadBackAsWritten(t *testing.T) {
	owner, contributor := MustName("owner"), MustName("contributor")
	a, b := keyOf(1).Principal(), keyOf(2).Principal()
	enroll := Enroll{Owner: owner, Roles: map[Name][]Capability{contributor: {MustCapability("rm own/* (level=1)")}, MustName("observer"): nil}}
	got := readBack(t, enroll, ReadEnroll)
	if got.Owner != owner || len(got.Roles) != 2 || got.Roles[contributor][0].String() != "rm own/* (level=1)" {
		t.Errorf("the enrollment reads back as %+v", got)
	}
	if p := got.Policy(a); p.Version() != 1 || len(p.Owners()) != 1 || p.Owners()[0] != a {
		t.Errorf("the enrollment's policy is %+v, want version 1 with %s its one owner", p, a)
	}

	prop := Propose{Base: 3, Members: map[PrincipalID]Name{b: contributor, a: owner}, Threshold: Fixed{N: 1}}
	back := readBack(t, prop, ReadPropose)
	if back.Base != 3 || back.Members[b] != contributor || back.Threshold != (Fixed{N: 1}) {
		t.Errorf("the proposal reads back as %+v", back)
	}
	if back.Digest() != prop.Digest() {
		t.Error("a proposal's digest changes across its wire")
	}
	if (Propose{Base: 4, Members: prop.Members, Threshold: prop.Threshold}).Digest() == prop.Digest() {
		t.Error("two proposals over different bases share a digest")
	}

	if ap := readBack(t, Approve{Proposal: prop.Digest()}, ReadApprove); ap.Proposal != prop.Digest() {
		t.Errorf("the approval cites %s, want %s", ap.Proposal, prop.Digest())
	}
	readBack(t, Request{}, ReadRequest)
}

func TestAnEnrollmentListingTheOwnerRoleIsRefused(t *testing.T) {
	owner := MustName("owner")
	_, err := ReadEnroll(wire.NewReader(payloadBytes(Enroll{Owner: owner, Roles: map[Name][]Capability{owner: nil}})))
	if err == nil || !strings.Contains(err.Error(), "is the owner role") {
		t.Errorf("%v, want the owner role refused as a listed role", err)
	}
}

func TestAnApprovalNamesItsProposalByAUniquePrefix(t *testing.T) {
	one := Propose{Base: 1, Members: map[PrincipalID]Name{}, Threshold: Majority{}}.Digest()
	two := Propose{Base: 2, Members: map[PrincipalID]Name{}, Threshold: Majority{}}.Digest()
	if d, err := ParseDigestPrefix(one.String()[:12], []Digest{one, two}); err != nil || d != one {
		t.Errorf("%s, %v, want %s", d, err, one)
	}
	if _, err := ParseDigestPrefix("", []Digest{one}); err == nil {
		t.Error("an empty prefix names a proposal")
	}
	if _, err := ParseDigestPrefix("zz", []Digest{one, two}); err == nil || !strings.Contains(err.Error(), "no pending") {
		t.Errorf("%v, want no pending proposal", err)
	}
}
