package events

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/promptctl/lit2/internal/events/wire"
)

// Verb is a write verb: what a capability grants and an intent does. get
// is no Verb, so a capability to read is unrepresentable: reads are the
// tiers' (§3.19) `[LAW:types-are-the-program]`.
type Verb uint8

const (
	Set Verb = iota + 1
	Add
	Rm
)

func (v Verb) String() string {
	switch v {
	case Set:
		return "set"
	case Add:
		return "add"
	case Rm:
		return "rm"
	}
	panic(fmt.Sprintf("events: %d is not a Verb", uint8(v)))
}

// Binding is which items a capability's subject matches: every item, the
// ones its holder created, the ones assigned to it, or either (§3.18).
type Binding uint8

const (
	All Binding = iota + 1
	Own
	Assignee
	OwnOrAssignee
)

func (b Binding) String() string {
	switch b {
	case All:
		return "*"
	case Own:
		return "own"
	case Assignee:
		return "assignee"
	case OwnOrAssignee:
		return "own-or-assignee"
	}
	panic(fmt.Sprintf("events: %d is not a Binding", uint8(b)))
}

// GlobStep is one step of a field glob: a literal segment, or `*`, any one
// segment.
//
//sumtype:decl
type GlobStep interface{ globStep() }

// Literal matches the one segment Segment.
type Literal struct{ Segment string }

// AnyOne matches any one segment.
type AnyOne struct{}

func (Literal) globStep() {}
func (AnyOne) globStep()  {}

// FieldGlob matches the field steps of an intent path: Steps one segment
// each, then, when Subtree, any number more. A glob's final `*` is its
// Subtree, as MQTT's `#` is, so `own/*` covers the item and everything
// beneath it, and `*/*/*` everything beneath an item but never the item
// (D4).
type FieldGlob struct {
	Steps   []GlobStep
	Subtree bool
}

// Matches is whether field, the steps after a subject, is in the glob.
func (g FieldGlob) Matches(field []string) bool {
	if len(field) < len(g.Steps) || !g.Subtree && len(field) != len(g.Steps) {
		return false
	}
	for i, s := range g.Steps {
		if l, ok := s.(Literal); ok && l.Segment != field[i] {
			return false
		}
	}
	return true
}

// Op is a condition's comparison.
type Op uint8

const (
	Eq Op = iota + 1
	Ne
)

// Condition is a fact a capability's subject must hold. Every condition is
// judged from facts the fold holds, so every verifier reaches the writer's
// verdict; a column stage, which only the writer's file knows, is never
// one (D3) `[LAW:single-enforcer]`.
//
//sumtype:decl
type Condition interface{ condition() }

// LevelIs holds when the subject's level compares to Level by Op.
type LevelIs struct {
	Op    Op
	Level Level
}

func (LevelIs) condition() {}

// Capability is `(verb, path glob, binding)` (§3.18), with the conditions
// its subject must hold. Only ParseCapability makes one, and String is its
// inverse, so the text a role is written in is the capability
// `[LAW:one-source-of-truth]`.
type Capability struct {
	Verb    Verb
	Binding Binding
	Field   FieldGlob
	Where   []Condition
}

// ParseCapability reads a capability:
//
//	capability := verb ' ' subject '/' glob (' (' atom (' ' atom)* ')')?
//	subject    := '*' | 'own' | 'assignee' | 'own-or-assignee'
//	glob       := step ('/' step)*     a step is a segment or '*'
//	atom       := 'level' ('=' | '!=') 1..5
//
// A final `*` in the glob is the subtree; one before it is any one
// segment. The subject matches items and homes, never the root, so no
// capability reaches a root path: those are the owner role's alone.
func ParseCapability(text string) (Capability, error) {
	fail := func(why error) (Capability, error) {
		return Capability{}, fmt.Errorf("%q is not a capability: %w", text, why)
	}
	body, atoms, hasAtoms := strings.Cut(text, " (")
	verbText, path, ok := strings.Cut(body, " ")
	if !ok {
		return fail(fmt.Errorf("a capability is a verb, a subject and a field glob, as `set own/*`: %w", errNoPath))
	}
	var c Capability
	switch verbText {
	case "set":
		c.Verb = Set
	case "add":
		c.Verb = Add
	case "rm":
		c.Verb = Rm
	default:
		return fail(fmt.Errorf("its verb is set, add or rm: %w", errVerb))
	}
	subject, glob, ok := strings.Cut(path, "/")
	if !ok || glob == "" {
		return fail(fmt.Errorf("its path is a subject, then / and a field glob: %w", errPath))
	}
	switch subject {
	case "*":
		c.Binding = All
	case "own":
		c.Binding = Own
	case "assignee":
		c.Binding = Assignee
	case "own-or-assignee":
		c.Binding = OwnOrAssignee
	default:
		return fail(fmt.Errorf("its subject is *, own, assignee or own-or-assignee: %w", errBinding))
	}
	steps := strings.Split(glob, "/")
	for i, s := range steps {
		switch {
		case s == "*" && i == len(steps)-1:
			c.Field.Subtree = true
		case s == "*":
			c.Field.Steps = append(c.Field.Steps, AnyOne{})
		case s == "":
			return fail(fmt.Errorf("its glob: %w", errEmptyStep))
		default:
			c.Field.Steps = append(c.Field.Steps, Literal{Segment: s})
		}
	}
	if !hasAtoms {
		return c, nil
	}
	inner, closed := strings.CutSuffix(atoms, ")")
	if !closed || inner == "" {
		return fail(fmt.Errorf("its conditions are atoms in parentheses, as (level=1): %w", errConditions))
	}
	for _, a := range strings.Split(inner, " ") {
		op, n := Eq, ""
		if v, ok := strings.CutPrefix(a, "level!="); ok {
			op, n = Ne, v
		} else if v, ok := strings.CutPrefix(a, "level="); ok {
			n = v
		} else {
			return fail(fmt.Errorf("%q is not a condition, level=N or level!=N: %w", a, errCondition))
		}
		i, err := strconv.Atoi(n)
		if err != nil {
			// [LAW:one-type-per-behavior] a level that is no number is a
			// condition outside the grammar, not a number outside the levels
			return fail(fmt.Errorf("%q is not a condition: its level is a number, 1 to 5: %w", a, errCondition))
		}
		l, err := ParseLevel(i)
		if err != nil {
			return fail(err)
		}
		c.Where = append(c.Where, LevelIs{Op: op, Level: l})
	}
	return c, nil
}

// The capability grammar's refusals, one per part of it; the grammar each
// part keeps is said where it is refused.
var (
	errNoPath     = errors.New("a capability with no path after its verb")
	errVerb       = errors.New("a verb outside a capability's")
	errPath       = errors.New("a path with no subject or no field glob")
	errBinding    = errors.New("a subject outside a capability's")
	errEmptyStep  = errors.New("a glob with an empty step")
	errConditions = errors.New("conditions outside parentheses, or none in them")
	errCondition  = errors.New("a condition outside a capability's")
)

// MustCapability is text, which the caller's own table holds as a
// capability; one that is not is the caller's bug, so it panics.
func MustCapability(text string) Capability {
	c, err := ParseCapability(text)
	if err != nil {
		panic(fmt.Sprintf("events: %v", err))
	}
	return c
}

// String is the capability as ParseCapability reads it.
func (c Capability) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", c.Verb, c.Binding)
	for _, s := range c.Field.Steps {
		switch s := s.(type) {
		case Literal:
			b.WriteString("/" + s.Segment)
		case AnyOne:
			b.WriteString("/*")
		}
	}
	if c.Field.Subtree {
		b.WriteString("/*")
	}
	atoms := make([]string, 0, len(c.Where))
	for _, w := range c.Where {
		switch w := w.(type) {
		case LevelIs:
			op := "="
			if w.Op == Ne {
				op = "!="
			}
			atoms = append(atoms, fmt.Sprintf("level%s%d", op, w.Level.Int()))
		}
	}
	if len(atoms) > 0 {
		fmt.Fprintf(&b, " (%s)", strings.Join(atoms, " "))
	}
	return b.String()
}

// Threshold is how many owners must approve a change to the owner set or
// to the threshold itself: a majority of them, one while there is one
// (§3.17), or a fixed number. Zero is no threshold, never "the default"
// `[LAW:types-are-the-program]`.
//
//sumtype:decl
type Threshold interface{ threshold() }

// Majority is more than half of the owners.
type Majority struct{}

// Fixed is N owners.
type Fixed struct{ N uint8 }

func (Majority) threshold() {}
func (Fixed) threshold()    {}

// ThresholdText is t as `get /policy` prints it and `set /policy/threshold`
// reads it.
func ThresholdText(t Threshold) string {
	switch t := t.(type) {
	case Majority:
		return "majority"
	case Fixed:
		return strconv.Itoa(int(t.N))
	}
	panic(fmt.Sprintf("events: %T is not a Threshold", t))
}

// ParseThreshold reads what ThresholdText writes.
func ParseThreshold(s string) (Threshold, error) {
	if s == "majority" {
		return Majority{}, nil
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%q is not a threshold: majority, or a number of owners from 1", s)
	}
	return fixedOf(n)
}

// errThresholdRange is a number of owners outside a byte's 1 to 255.
var errThresholdRange = errors.New("a number of owners outside a threshold's range")

// fixedOf is the threshold of n owners, from 1 to 255: the one judge the
// text and the wire readers share, so no two spellings read as one
// threshold `[LAW:single-enforcer]`.
func fixedOf(n uint64) (Threshold, error) {
	if n < 1 || n > 255 {
		return nil, fmt.Errorf("%d is not a threshold, a number of owners from 1 to 255: %w", n, errThresholdRange)
	}
	return Fixed{N: uint8(n)}, nil
}

// Policy is the policy document (§3.17): who is a member and in which
// role, what each role may do, and how many owners approve a change to
// the owners. The owner role is named, never listed: it grants every path,
// by construction. Only NewPolicy makes one, and it refuses a policy that
// locks its workspace out, so a Policy always has an owner and every
// member a declared role `[LAW:parse-dont-validate]`.
type Policy struct {
	version   PolicyVersion
	owner     Name
	roles     map[Name][]Capability
	members   map[PrincipalID]Name
	threshold Threshold
}

// The refusals of a policy that locks its workspace out. errOwnerRoleListed
// is one refusal wherever it is met, as NewPolicy and ReadEnroll meet it.
var (
	errOwnerRoleListed     = errors.New("a listed owner role")
	errUndeclaredRole      = errors.New("a role the policy does not declare")
	errNoOwner             = errors.New("a policy with no owner")
	errThresholdOverOwners = errors.New("a threshold beyond the owners")
)

// NewPolicy is the policy at version v, the one judge of a policy: at
// least one member holds the owner role, every member's role is the owner
// or one of roles, roles never names the owner, and a fixed threshold is
// within the owners.
func NewPolicy(v PolicyVersion, owner Name, roles map[Name][]Capability, members map[PrincipalID]Name, t Threshold) (Policy, error) {
	if _, ok := roles[owner]; ok {
		return Policy{}, fmt.Errorf("the role %s is the owner role, which grants every path and lists none: %w", owner, errOwnerRoleListed)
	}
	owners := 0
	for _, p := range slices.SortedFunc(maps.Keys(members), ComparePrincipals) {
		r := members[p]
		if r == owner {
			owners++
			continue
		}
		if _, ok := roles[r]; !ok {
			return Policy{}, fmt.Errorf("%s's role %s is not a role this policy declares: %w", p, r, errUndeclaredRole)
		}
	}
	if owners == 0 {
		return Policy{}, fmt.Errorf("no member holds the owner role %s: %w", owner, errNoOwner)
	}
	if f, ok := t.(Fixed); ok && (f.N == 0 || int(f.N) > owners) {
		return Policy{}, fmt.Errorf("a threshold of %d owners is more than the %d owners there are: %w", f.N, owners, errThresholdOverOwners)
	}
	return Policy{version: v, owner: owner, roles: maps.Clone(roles), members: maps.Clone(members), threshold: t}, nil
}

// Version is the policy's version: 1 at the enrollment, its base plus one
// at each activation.
func (p Policy) Version() PolicyVersion { return p.version }

// Owner is the owner role's name.
func (p Policy) Owner() Name { return p.owner }

// Threshold is the owner threshold.
func (p Policy) Threshold() Threshold { return p.threshold }

// Role is principal's role, and false when it is no member.
func (p Policy) Role(principal PrincipalID) (Name, bool) {
	r, ok := p.members[principal]
	return r, ok
}

// Grants is role's capabilities; the owner role's are every path, which
// no list holds.
func (p Policy) Grants(role Name) []Capability { return p.roles[role] }

// Roles is the declared roles other than the owner, by name.
func (p Policy) Roles() []Name {
	return slices.SortedFunc(maps.Keys(p.roles), func(a, b Name) int { return strings.Compare(a.s, b.s) })
}

// Declares is whether role is the owner or one of the policy's roles.
func (p Policy) Declares(role Name) bool {
	_, ok := p.roles[role]
	return ok || role == p.owner
}

// Membership is one member and its role.
type Membership struct {
	Principal PrincipalID
	Role      Name
}

// Members is every member, in principal order.
func (p Policy) Members() []Membership {
	out := make([]Membership, 0, len(p.members))
	for _, k := range slices.SortedFunc(maps.Keys(p.members), ComparePrincipals) {
		out = append(out, Membership{Principal: k, Role: p.members[k]})
	}
	return out
}

// Owners is every member holding the owner role, in principal order.
func (p Policy) Owners() []PrincipalID {
	var out []PrincipalID
	for _, m := range p.Members() {
		if m.Role == p.owner {
			out = append(out, m.Principal)
		}
	}
	return out
}

// Next is the policy a proposal over p makes: its roles, with members and
// t, at version p's plus one; NewPolicy judges it.
func (p Policy) Next(members map[PrincipalID]Name, t Threshold) (Policy, error) {
	return NewPolicy(p.version+1, p.owner, p.roles, members, t)
}

// Proposal is what a proposal over p would carry for next, p's members
// and threshold replaced.
func (p Policy) Proposal(next Policy) Propose {
	return Propose{Base: p.Digest(), Members: maps.Clone(next.members), Threshold: next.threshold}
}

// Digest is the policy's SHA-256 over its version, its members in
// principal order and its threshold: what a proposal names as its base, so
// two versions of one number that differ, each activated on a replica
// before they met, never stand for each other.
func (p Policy) Digest() Digest {
	var w wire.Writer
	w.Uvarint(1, uint64(p.version))
	writeChange(&w, p.members, p.threshold)
	return sha256.Sum256(append([]byte("lit2 policy v1\x00"), w.Encoded()...))
}

// MemberMap is the members as a map, a copy, for a proposal to change.
func (p Policy) MemberMap() map[PrincipalID]Name { return maps.Clone(p.members) }

// ComparePrincipals is the principal order: by suite, then by key.
func ComparePrincipals(a, b PrincipalID) int {
	if a.suite != b.suite {
		return int(a.suite) - int(b.suite)
	}
	return strings.Compare(a.key, b.key)
}

// Enroll is policy.enroll, the first event of a workspace: the founding
// owner, its signer, enrolls itself as the one owner of the version-one
// policy, under the owner role Owner, with the other roles Roles and a
// majority threshold (§3.17). The signer is the founder, so an enrollment
// naming another founder is unrepresentable.
type Enroll struct {
	Owner   Name
	Roles   map[Name][]Capability
	unknown wire.Unknown
}

// Policy is the version-one policy an enrollment signed by founder makes.
func (p Enroll) Policy(founder PrincipalID) Policy {
	pol, err := NewPolicy(1, p.Owner, p.Roles, map[PrincipalID]Name{founder: p.Owner}, Majority{})
	if err != nil {
		// ReadEnroll refuses the only enrollment NewPolicy could
		panic(fmt.Sprintf("events: an enrollment that read makes no policy: %v", err))
	}
	return pol
}

// Propose is policy.propose: the policy whose digest is Base with its
// members and threshold replaced. The roles stay the enrollment's, so no
// proposal can edit a role.
type Propose struct {
	Base      Digest
	Members   map[PrincipalID]Name
	Threshold Threshold
	unknown   wire.Unknown
}

// Digest is a SHA-256: a policy's, which a proposal names as its base, or
// a proposal's, which an approval cites, over its base, its members in
// principal order and its threshold, so it binds the base policy and two
// equal proposals are one target (D10).
type Digest [32]byte

func (d Digest) String() string { return fmt.Sprintf("%x", d[:]) }

// Digest is the proposal's digest.
func (p Propose) Digest() Digest {
	var w wire.Writer
	p.canonical(&w)
	return sha256.Sum256(append([]byte("lit2 proposal v1\x00"), w.Encoded()...))
}

// Approve is policy.approve: its signer approves the proposal Proposal.
type Approve struct {
	Proposal Digest
	unknown  wire.Unknown
}

// Request is policy.request: its signer, no member, asks to be one. The
// key is the signer's, so a request is self-certifying.
type Request struct{ unknown wire.Unknown }

func (Enroll) Kind() Kind  { return "policy.enroll" }
func (Propose) Kind() Kind { return "policy.propose" }
func (Approve) Kind() Kind { return "policy.approve" }
func (Request) Kind() Kind { return "policy.request" }

func (Enroll) Schema() SchemaVersion  { return 1 }
func (Propose) Schema() SchemaVersion { return 1 }
func (Approve) Schema() SchemaVersion { return 1 }
func (Request) Schema() SchemaVersion { return 1 }

// policy.enroll = 1:owner 2:role{1:name 2:capability (its text, repeated)}
// (repeated, by name) · unknown….
func (p Enroll) AppendTo(w *wire.Writer) {
	w.Bytes(1, nameWire(p.Owner))
	for _, n := range slices.SortedFunc(maps.Keys(p.Roles), func(a, b Name) int { return strings.Compare(a.s, b.s) }) {
		var r wire.Writer
		r.Bytes(1, nameWire(n))
		for _, c := range p.Roles[n] {
			r.Bytes(2, []byte(c.String()))
		}
		w.Bytes(2, r.Encoded())
	}
	w.Keep(p.unknown)
}

// enrollment is an enrollment's fields: its owner role and its roles, in
// stored order.
type enrollment struct {
	owner Name
	roles []role
}

// enrollments is policy.enroll at version 1: an owner role, and roles that
// never name it, each once.
var enrollments = declare("a policy.enroll", []field[enrollment]{
	nameField(1, partOwnerRole, one, func(e *enrollment) *Name { return &e.owner }),
	{2, partRole, many, func(e *enrollment, r *wire.Reader) error {
		ro, err := readRole(r.Bytes())
		e.roles = append(e.roles, ro)
		return err
	}},
}, func(e *enrollment, u wire.Unknown) (Versioned, error) {
	p := Enroll{Owner: e.owner, Roles: map[Name][]Capability{}, unknown: u}
	for _, ro := range e.roles {
		switch _, ok := p.Roles[ro.name]; {
		case ok:
			return nil, fmt.Errorf("the role %s: %w", ro.name, twice{What: partRole})
		case ro.name == p.Owner:
			return nil, fmt.Errorf("the role %s is the owner role, which grants every path and lists none: %w", p.Owner, errOwnerRoleListed)
		}
		p.Roles[ro.name] = ro.caps
	}
	return p, nil
})

// ReadEnroll reads policy.enroll at version 1.
func ReadEnroll(r *wire.Reader) (Versioned, error) { return enrollments.decode(r) }

// role is one role an enrollment declares: its name and what it grants.
type role struct {
	name Name
	caps []Capability
}

// readRole reads a role's stored form.
var readRole = declare("a role", []field[role]{
	nameField(1, partRoleName, one, func(ro *role) *Name { return &ro.name }),
	{2, partCapability, many, func(ro *role, r *wire.Reader) error {
		c, err := ParseCapability(string(r.Bytes()))
		ro.caps = append(ro.caps, c)
		return err
	}},
}, whole).read

// policy.propose = 1:base 2:member{1:principal 2:role} (repeated, in
// principal order) 3:threshold · unknown….
func (p Propose) AppendTo(w *wire.Writer) {
	p.canonical(w)
	w.Keep(p.unknown)
}

// The threshold's wire fields: 1:majority (empty) | 2:fixed (its N).
const (
	thresholdMajority = 1
	thresholdFixed    = 2
)

func (p Propose) canonical(w *wire.Writer) {
	w.Bytes(1, p.Base[:])
	writeChange(w, p.Members, p.Threshold)
}

// writeChange is members in principal order, then the threshold: what a
// proposal sets and a policy holds, written alike for both digests.
func writeChange(w *wire.Writer, members map[PrincipalID]Name, threshold Threshold) {
	for _, k := range slices.SortedFunc(maps.Keys(members), ComparePrincipals) {
		var m wire.Writer
		m.Bytes(1, k.wire())
		m.Bytes(2, nameWire(members[k]))
		w.Bytes(2, m.Encoded())
	}
	var t wire.Writer
	switch th := threshold.(type) {
	case Majority:
		t.Bytes(thresholdMajority, nil)
	case Fixed:
		t.Uvarint(thresholdFixed, uint64(th.N))
	default:
		panic(fmt.Sprintf("events: %T is not a Threshold", threshold))
	}
	w.Bytes(3, t.Encoded())
}

// proposal is a proposal's fields: its base, its members in stored order
// and its threshold.
type proposal struct {
	base      Digest
	members   []membership
	threshold Threshold
}

// proposals is policy.propose at version 1: a base, members each named
// once, and a threshold.
var proposals = declare("a policy.propose", []field[proposal]{
	fixedField(1, partBase, one, func(p *proposal) []byte { return p.base[:] }),
	{2, partMember, many, func(p *proposal, r *wire.Reader) error {
		m, err := readMembership(r.Bytes())
		p.members = append(p.members, m)
		return err
	}},
	{3, partThreshold, one, func(p *proposal, r *wire.Reader) (err error) {
		p.threshold, err = readThreshold(r.Bytes())
		return err
	}},
}, func(p *proposal, u wire.Unknown) (Versioned, error) {
	out := Propose{Base: p.base, Members: map[PrincipalID]Name{}, Threshold: p.threshold, unknown: u}
	for _, m := range p.members {
		if _, ok := out.Members[m.who]; ok {
			return nil, fmt.Errorf("the member %s: %w", m.who, twice{What: partMember})
		}
		out.Members[m.who] = m.role
	}
	return out, nil
})

// ReadPropose reads policy.propose at version 1.
func ReadPropose(r *wire.Reader) (Versioned, error) { return proposals.decode(r) }

// membership is one member a proposal names: the principal and its role.
type membership struct {
	who  PrincipalID
	role Name
}

// readMembership reads a member's stored form.
var readMembership = declare("a member", []field[membership]{
	{1, partPrincipal, one, func(m *membership, r *wire.Reader) (err error) {
		m.who, err = readPrincipal(r.Bytes())
		return err
	}},
	nameField(2, partRole, one, func(m *membership) *Name { return &m.role }),
}, whole).read

// readThreshold reads a threshold's stored form: one of a majority, which
// carries nothing, and a fixed number. The fold activates a policy by it,
// so one in a form this binary does not know is a payload it cannot read:
// refused, and its event Unread (INV:preserve-unknown), never a policy
// with no threshold.
var readThreshold = declare("a threshold", []field[Threshold]{
	{thresholdMajority, partMajority, optional, func(th *Threshold, r *wire.Reader) error {
		*th = Majority{}
		return sized(partMajority, r.Bytes(), 0)
	}},
	{thresholdFixed, partFixed, optional, func(th *Threshold, r *wire.Reader) (err error) {
		*th, err = fixedOf(r.Uvarint())
		return err
	}},
}, whole, forms{partThreshold, one, []uint32{thresholdMajority, thresholdFixed}}).read

// policy.approve = 1:proposal (its digest) · unknown….
func (p Approve) AppendTo(w *wire.Writer) {
	w.Bytes(1, p.Proposal[:])
	w.Keep(p.unknown)
}

// approvals is policy.approve at version 1.
var approvals = declare("a policy.approve", []field[Approve]{
	fixedField(1, partProposal, one, func(p *Approve) []byte { return p.Proposal[:] }),
}, func(p *Approve, u wire.Unknown) (Versioned, error) {
	p.unknown = u
	return *p, nil
})

// ReadApprove reads policy.approve at version 1.
func ReadApprove(r *wire.Reader) (Versioned, error) { return approvals.decode(r) }

// policy.request = unknown….
func (p Request) AppendTo(w *wire.Writer) { w.Keep(p.unknown) }

// requests is policy.request at version 1, which has no field of its own.
var requests = declare("a policy.request", nil, func(p *Request, u wire.Unknown) (Versioned, error) {
	p.unknown = u
	return *p, nil
})

// ReadRequest reads policy.request at version 1.
func ReadRequest(r *wire.Reader) (Versioned, error) { return requests.decode(r) }

// The refusals of a proposal's name: none given, or one no pending
// proposal's digest starts.
var (
	errEmptyDigestPrefix = errors.New("an empty digest prefix")
	errNoPendingProposal = errors.New("a digest prefix no pending proposal's digest starts with")
)

// ParseDigestPrefix is the one digest among ds that hex starts, as an
// approval names a pending proposal.
func ParseDigestPrefix(hex string, ds []Digest) (Digest, error) {
	if hex == "" {
		return Digest{}, fmt.Errorf("a proposal is named by its digest or a prefix of it: %w", errEmptyDigestPrefix)
	}
	var found []Digest
	for _, d := range ds {
		if strings.HasPrefix(d.String(), hex) {
			found = append(found, d)
		}
	}
	switch len(found) {
	case 0:
		return Digest{}, fmt.Errorf("%s: %w", hex, errNoPendingProposal)
	case 1:
		return found[0], nil
	}
	return Digest{}, fmt.Errorf("%s starts %d pending proposals' digests", hex, len(found))
}
