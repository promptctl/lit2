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
		return fail(errors.New("a capability is a verb, a subject and a field glob, as `set own/*`"))
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
		return fail(errVerb)
	}
	subject, glob, ok := strings.Cut(path, "/")
	if !ok || glob == "" {
		return fail(errPath)
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
		return fail(errBinding)
	}
	steps := strings.Split(glob, "/")
	for i, s := range steps {
		switch {
		case s == "*" && i == len(steps)-1:
			c.Field.Subtree = true
		case s == "*":
			c.Field.Steps = append(c.Field.Steps, AnyOne{})
		case s == "":
			return fail(errEmptyStep)
		default:
			c.Field.Steps = append(c.Field.Steps, Literal{Segment: s})
		}
	}
	if !hasAtoms {
		return c, nil
	}
	inner, closed := strings.CutSuffix(atoms, ")")
	if !closed || inner == "" {
		return fail(errConditions)
	}
	for _, a := range strings.Split(inner, " ") {
		op, n := Eq, ""
		if v, ok := strings.CutPrefix(a, "level!="); ok {
			op, n = Ne, v
		} else if v, ok := strings.CutPrefix(a, "level="); ok {
			n = v
		} else {
			return fail(fmt.Errorf("%q is not a condition: %w", a, errCondition))
		}
		i, err := strconv.Atoi(n)
		if err != nil {
			return fail(fmt.Errorf("%q: a level is a number, %w", a, ErrNotALevel))
		}
		l, err := ParseLevel(i)
		if err != nil {
			return fail(err)
		}
		c.Where = append(c.Where, LevelIs{Op: op, Level: l})
	}
	return c, nil
}

// The capability grammar's refusals, one per part of it.
var (
	errVerb       = errors.New("its verb is set, add or rm")
	errPath       = errors.New("its path is a subject, then / and a field glob")
	errBinding    = errors.New("its subject is *, own, assignee or own-or-assignee")
	errEmptyStep  = errors.New("its glob has an empty step")
	errConditions = errors.New("its conditions are atoms in parentheses, as (level=1)")
	errCondition  = errors.New("level=N or level!=N")
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
var errThresholdRange = errors.New("a number of owners is from 1 to 255")

// fixedOf is the threshold of n owners, from 1 to 255: the one judge the
// text and the wire readers share, so no two spellings read as one
// threshold `[LAW:single-enforcer]`.
func fixedOf(n uint64) (Threshold, error) {
	if n < 1 || n > 255 {
		return nil, fmt.Errorf("%d is not a threshold: %w", n, errThresholdRange)
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
	errOwnerRoleListed     = errors.New("is the owner role, which grants every path and lists none")
	errUndeclaredRole      = errors.New("is not a role this policy declares")
	errNoOwner             = errors.New("no member holds the owner role")
	errThresholdOverOwners = errors.New("a threshold beyond the owners")
)

// NewPolicy is the policy at version v, the one judge of a policy: at
// least one member holds the owner role, every member's role is the owner
// or one of roles, roles never names the owner, and a fixed threshold is
// within the owners.
func NewPolicy(v PolicyVersion, owner Name, roles map[Name][]Capability, members map[PrincipalID]Name, t Threshold) (Policy, error) {
	if _, ok := roles[owner]; ok {
		return Policy{}, fmt.Errorf("the role %s %w", owner, errOwnerRoleListed)
	}
	owners := 0
	for _, p := range slices.SortedFunc(maps.Keys(members), ComparePrincipals) {
		r := members[p]
		if r == owner {
			owners++
			continue
		}
		if _, ok := roles[r]; !ok {
			return Policy{}, fmt.Errorf("%s's role %s %w", p, r, errUndeclaredRole)
		}
	}
	if owners == 0 {
		return Policy{}, fmt.Errorf("%w %s", errNoOwner, owner)
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

// ReadEnroll reads policy.enroll at version 1: an owner role, and roles
// that never name it.
func ReadEnroll(r *wire.Reader) (Versioned, error) {
	p := Enroll{Roles: map[Name][]Capability{}}
	var hasOwner bool
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		switch tag {
		case 1:
			if hasOwner {
				return fmt.Errorf("it gives its owner role %w", errTwice)
			}
			n, err := ParseName(string(r.Bytes()))
			p.Owner, hasOwner = n, true
			return err
		case 2:
			name, caps, err := readRole(r.Bytes())
			if err != nil {
				return err
			}
			if _, ok := p.Roles[name]; ok {
				return fmt.Errorf("it declares the role %s %w", name, errTwice)
			}
			p.Roles[name] = caps
		}
		return nil
	})
	p.unknown = r.Rest()
	_, ownerListed := p.Roles[p.Owner]
	switch {
	case err != nil:
	case !hasOwner:
		err = fmt.Errorf("it names no owner role: %w", errMissing)
	case ownerListed:
		err = fmt.Errorf("the role %s %w", p.Owner, errOwnerRoleListed)
	}
	if err != nil {
		return nil, fmt.Errorf("a policy.enroll: %w", err)
	}
	return p, nil
}

func readRole(b []byte) (Name, []Capability, error) {
	var name Name
	var caps []Capability
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			name, err = ParseName(string(r.Bytes()))
		case 2:
			var c Capability
			c, err = ParseCapability(string(r.Bytes()))
			caps = append(caps, c)
		}
		return err
	})
	if err == nil && name.s == "" {
		err = fmt.Errorf("a role has no name: %w", errMissing)
	}
	if err != nil {
		return Name{}, nil, fmt.Errorf("a role: %w", err)
	}
	return name, caps, nil
}

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

// ReadPropose reads policy.propose at version 1.
func ReadPropose(r *wire.Reader) (Versioned, error) {
	p := Propose{Members: map[PrincipalID]Name{}}
	var hasBase bool
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			b := r.Bytes()
			if hasBase || len(b) != len(p.Base) {
				return errors.New("it names one base policy's 32-byte digest")
			}
			copy(p.Base[:], b)
			hasBase = true
		case 2:
			var who PrincipalID
			var role Name
			if who, role, err = readMembership(r.Bytes()); err == nil {
				if _, ok := p.Members[who]; ok {
					return fmt.Errorf("it names the member %s %w", who, errTwice)
				}
				p.Members[who] = role
			}
		case 3:
			if p.Threshold != nil {
				return fmt.Errorf("it gives its threshold %w", errTwice)
			}
			p.Threshold, err = readThreshold(r.Bytes())
		}
		return err
	})
	p.unknown = r.Rest()
	switch {
	case err != nil:
	case !hasBase:
		err = fmt.Errorf("it names no base policy: %w", errMissing)
	case p.Threshold == nil:
		err = fmt.Errorf("it gives no threshold: %w", errMissing)
	}
	if err != nil {
		return nil, fmt.Errorf("a policy.propose: %w", err)
	}
	return p, nil
}

func readMembership(b []byte) (PrincipalID, Name, error) {
	var who PrincipalID
	var role Name
	var hasWho bool
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		var err error
		switch tag {
		case 1:
			who, err = readPrincipal(r.Bytes())
			hasWho = true
		case 2:
			role, err = ParseName(string(r.Bytes()))
		}
		return err
	})
	switch {
	case err != nil:
	case !hasWho:
		err = fmt.Errorf("it names no principal: %w", errMissing)
	case role.s == "":
		err = fmt.Errorf("it names no role: %w", errMissing)
	}
	if err != nil {
		return PrincipalID{}, Name{}, fmt.Errorf("a member: %w", err)
	}
	return who, role, nil
}

func readThreshold(b []byte) (Threshold, error) {
	var made []Threshold
	err := wire.NewReader(b).Each(func(tag uint32, r *wire.Reader) error {
		switch tag {
		case thresholdMajority:
			r.Bytes()
			made = append(made, Majority{})
		case thresholdFixed:
			f, err := fixedOf(r.Uvarint())
			if err != nil {
				return err
			}
			made = append(made, f)
		}
		return nil
	})
	switch {
	case err != nil:
		return nil, fmt.Errorf("a threshold: %w", err)
	case len(made) != 1:
		return nil, fmt.Errorf("a threshold is a majority or a number; this one is %d of them: %w", len(made), errForms)
	}
	return made[0], nil
}

// policy.approve = 1:proposal (its digest) · unknown….
func (p Approve) AppendTo(w *wire.Writer) {
	w.Bytes(1, p.Proposal[:])
	w.Keep(p.unknown)
}

// ReadApprove reads policy.approve at version 1.
func ReadApprove(r *wire.Reader) (Versioned, error) {
	var p Approve
	var has bool
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		if tag != 1 {
			return nil
		}
		b := r.Bytes()
		if has || len(b) != len(p.Proposal) {
			return errors.New("it cites one proposal's 32-byte digest")
		}
		copy(p.Proposal[:], b)
		has = true
		return nil
	})
	p.unknown = r.Rest()
	if err == nil && !has {
		err = fmt.Errorf("it cites no proposal: %w", errMissing)
	}
	if err != nil {
		return nil, fmt.Errorf("a policy.approve: %w", err)
	}
	return p, nil
}

// policy.request = unknown….
func (p Request) AppendTo(w *wire.Writer) { w.Keep(p.unknown) }

// ReadRequest reads policy.request at version 1.
func ReadRequest(r *wire.Reader) (Versioned, error) {
	err := r.Each(func(uint32, *wire.Reader) error { return nil })
	if err != nil {
		return nil, fmt.Errorf("a policy.request: %w", err)
	}
	return Request{unknown: r.Rest()}, nil
}

// The refusals of a proposal's name: none given, or one no pending
// proposal's digest starts.
var (
	errEmptyDigestPrefix = errors.New("a proposal is named by its digest or a prefix of it")
	errNoPendingProposal = errors.New("no pending proposal's digest starts")
)

// ParseDigestPrefix is the one digest among ds that hex starts, as an
// approval names a pending proposal.
func ParseDigestPrefix(hex string, ds []Digest) (Digest, error) {
	if hex == "" {
		return Digest{}, errEmptyDigestPrefix
	}
	var found []Digest
	for _, d := range ds {
		if strings.HasPrefix(d.String(), hex) {
			found = append(found, d)
		}
	}
	switch len(found) {
	case 0:
		return Digest{}, fmt.Errorf("%w %s", errNoPendingProposal, hex)
	case 1:
		return found[0], nil
	}
	return Digest{}, fmt.Errorf("%s starts %d pending proposals' digests", hex, len(found))
}
