package verbs

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/promptctl/lit2/internal/address"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/field"
	"github.com/promptctl/lit2/internal/fold"
)

// The governed paths: the policy's members, a member's role, the owner
// threshold and the approvals, each written as a proposal of the policy's
// next version or an approval of one (architecture.md §3.18).
const (
	membersPath   field.Path = "members"
	rolePath      field.Path = "role"
	thresholdPath field.Path = "policy/threshold"
	approvalsPath field.Path = "policy/approvals"
)

// Member is a member of the policy, which no event is about: a write of
// it is the policy's, about the root.
type Member struct {
	ID       events.PrincipalID
	Rendered string
}

func (Member) who() {}

// Proposed is the policy's next version proposed: its digest, the version
// it is based on, and each path it writes.
type Proposed struct {
	Digest events.Digest
	Base   events.PolicyVersion
	Writes []fold.PathWrite
}

// Approved is the writer's approval of a proposal, by its digest.
type Approved struct {
	Digest events.Digest
	Base   events.PolicyVersion
}

func (Proposed) change() {}
func (Approved) change() {}

// policyClaim is what every proposal overwrites: the policy's next version,
// which one command proposes once.
var policyClaim = claim{who: events.Root{}, path: "policy"}

// inForce is the policy in force in w. A verb runs only over an enrolled
// store, which the root refuses to open otherwise.
func (w World) inForce() events.Policy { return w.State().Policy().(fold.Enrolled).Policy }

// governedReads is every governed field's reading, from the policy in
// force: each member and its role, a member's role, the threshold, and
// each approval of a proposal not yet in force.
var governedReads = map[field.Path]func(World, address.Node) []string{
	membersPath: func(w World, _ address.Node) []string {
		var out []string
		for _, m := range w.inForce().Members() {
			out = append(out, fmt.Sprintf("%s %s", m.Principal, m.Role))
		}
		return out
	},
	rolePath: func(w World, n address.Node) []string {
		role, _ := w.inForce().Role(n.(address.Principal).ID) // a path names a member only
		return []string{role.String()}
	},
	thresholdPath: func(w World, _ address.Node) []string {
		return []string{events.ThresholdText(w.inForce().Threshold())}
	},
	approvalsPath: func(w World, _ address.Node) []string {
		var out []string
		for _, pr := range w.State().Proposals() {
			for _, a := range pr.Approvals {
				out = append(out, fmt.Sprintf("%s %s", shortDigest(pr.Digest), a))
			}
		}
		return out
	},
}

// shortDigest is a digest as a reader names it: a prefix any command resolves.
func shortDigest(d events.Digest) string { return d.String()[:12] }

// requestsRow is /members/requests: each principal that asked to join.
func requestsRow(w World, _ address.Node) ([]string, error) {
	var out []string
	for _, r := range w.State().Requests() {
		out = append(out, r.Principal.String())
	}
	return out, nil
}

// policyRow is /policy: the version in force and its members.
func policyRow(w World, _ address.Node) ([]string, error) {
	p := w.inForce()
	return []string{fmt.Sprintf("version %d · %d members", p.Version(), len(p.Members()))}, nil
}

// proposalsRow is /policy/proposals: each pending proposal, what it
// writes and what it lacks.
func proposalsRow(w World, _ address.Node) ([]string, error) {
	var out []string
	for _, pr := range w.State().Proposals() {
		out = append(out, fmt.Sprintf("%s · %s · %s", shortDigest(pr.Digest), writes(w, fold.Diff(pr.Base, pr.Next)), lacks(pr)))
	}
	return out, nil
}

// integrityRow is /integrity: each event the fold carried, and a count per
// reason.
func integrityRow(w World, _ address.Node) ([]string, error) {
	carried := w.State().Integrity().Carried
	var out []string
	count := map[string]int{}
	for _, c := range carried {
		reason := fold.ReasonName(c.Reason)
		count[reason]++
		out = append(out, fmt.Sprintf("%s by %s (checkout %s) at %s: %s", c.Event, c.Principal, c.Writer, c.RecordedAt.Format("2006-01-02T15:04:05Z07:00"), reason))
	}
	for _, r := range slices.Sorted(maps.Keys(count)) {
		out = append(out, fmt.Sprintf("carried %s: %d", r, count[r]))
	}
	if len(carried) == 0 {
		out = append(out, "carried: none")
	}
	return out, nil
}

// writes is each path a proposal writes, as its operands are typed.
func writes(w World, ws []fold.PathWrite) string {
	out := make([]string, len(ws))
	for i, pw := range ws {
		out[i] = fmt.Sprintf("%s %s", pw.Verb, w.pathOf(pw))
	}
	return strings.Join(out, ", ")
}

// lacks is what a proposal waits on: a base another version passed, or the
// approvals its requirement still needs.
func lacks(pr fold.Proposal) string {
	if pr.Stale {
		return fmt.Sprintf("stale: version %d is no longer in force", pr.Base.Version())
	}
	switch need := pr.Need.(type) {
	case fold.OwnerQuorum:
		have, incoming := need.Tally(pr.Approvals)
		text := fmt.Sprintf("waits on %d of the owners, has %d", need.Need, have)
		if len(incoming) > 0 {
			missing := make([]string, len(incoming))
			for i, in := range incoming {
				missing[i] = in.String()
			}
			text += ", and on each incoming owner: " + strings.Join(missing, ", ")
		}
		return text
	case fold.OneHolder:
		return "waits on one member granted every path it writes"
	}
	panic(fmt.Sprintf("verbs: %T is not a Requirement", pr.Need))
}

// govern compiles a governed write: the policy in force with the one change
// it names, proposed as the next version, and approved in the same command
// when the writer's approval counts toward it; or an approval of a
// proposal. The fold judges both, as every replica does, so govern only
// shapes them.
func govern(w World, wr write) ([]piece, []Change, []claim, *refusal) {
	in := w.inForce()
	members, threshold := in.MemberMap(), in.Threshold()
	switch {
	case wr.path == membersPath && wr.shape == governAdd:
		id, role, r := memberOf(in, wr.value)
		if r != nil {
			return nil, nil, nil, r
		}
		members[id] = role
	case wr.path == membersPath && wr.shape == governRm:
		delete(members, wr.subject.At.(Member).ID)
	case wr.path == rolePath:
		role, r := roleOf(in, wr.value)
		if r != nil {
			return nil, nil, nil, r
		}
		members[wr.subject.At.(Member).ID] = role
	case wr.path == thresholdPath:
		s, ok := wr.value.(field.Scalar)
		if !ok {
			return nil, nil, nil, refuse("value", "%s holds one value", wr.path)
		}
		t, err := events.ParseThreshold(s.Text)
		if err != nil {
			return nil, nil, nil, refuse("value", "%s: %v", wr.path, err)
		}
		threshold = t
	case wr.path == approvalsPath:
		return approve(w, wr)
	default:
		panic(fmt.Sprintf("verbs: %s is no governed write of its shape", wr.path))
	}
	next, err := in.Next(members, threshold)
	if err != nil {
		return nil, nil, nil, refuse("value", "%s: %v", wr.path, err)
	}
	diff := fold.Diff(in, next)
	if len(diff) == 0 {
		return nil, nil, []claim{policyClaim}, nil // the policy already reads so
	}
	prop := in.Proposal(next)
	pieces := []piece{fact{prop}}
	changes := []Change{Proposed{Digest: prop.Digest(), Base: in.Version(), Writes: diff}}
	if w.State().Approves(next, w.Me) {
		pieces = append(pieces, fact{events.Approve{Proposal: prop.Digest()}})
		changes = append(changes, Approved{Digest: prop.Digest(), Base: in.Version()})
	}
	return pieces, changes, []claim{policyClaim}, nil
}

// memberOf reads an added member, {principal: …, role: …}: a principal
// the policy does not list, in a role it declares.
func memberOf(in events.Policy, v field.Literal) (events.PrincipalID, events.Name, *refusal) {
	o, ok := v.(field.Object)
	if !ok {
		return events.PrincipalID{}, events.Name{}, refuse("value", `a member is {principal: "did:key:…", role: …}`)
	}
	var id events.PrincipalID
	var role events.Name
	var have []string
	for _, p := range o.Pairs {
		s, ok := p.Value.(field.Scalar)
		if !ok {
			return events.PrincipalID{}, events.Name{}, refuse("value", "a member's %s is one value", p.Key)
		}
		switch p.Key {
		case "principal":
			parsed, err := events.ParsePrincipalID(s.Text)
			if err != nil {
				return events.PrincipalID{}, events.Name{}, refuse("value", "principal: %v", err)
			}
			id = parsed
		case "role":
			r, no := roleOf(in, s)
			if no != nil {
				return events.PrincipalID{}, events.Name{}, no
			}
			role = r
		default:
			return events.PrincipalID{}, events.Name{}, refuse("value", "a member has a principal and a role, and %s is neither", p.Key)
		}
		have = append(have, p.Key)
	}
	if !slices.Contains(have, "principal") || !slices.Contains(have, "role") {
		return events.PrincipalID{}, events.Name{}, refuse("value", `a member is {principal: "did:key:…", role: …}`)
	}
	if held, ok := in.Role(id); ok {
		return events.PrincipalID{}, events.Name{}, refuse("value", "%s is already a member, as %s: set /members/%s/role moves it", id, held, id)
	}
	return id, role, nil
}

// roleOf reads a role the policy declares.
func roleOf(in events.Policy, v field.Literal) (events.Name, *refusal) {
	s, ok := v.(field.Scalar)
	if !ok {
		return events.Name{}, refuse("value", "a role is one value")
	}
	n, err := events.ParseName(s.Text)
	if err != nil || !in.Declares(n) {
		roles := make([]string, len(in.Roles()))
		for i, r := range in.Roles() {
			roles[i] = r.String()
		}
		return events.Name{}, refuse("value", "%q is no role of this policy; its roles are %s", s.Text, strings.Join(roles, ", "))
	}
	return n, nil
}

// approve is an approval of the proposal a digest prefix names, among
// those not yet in force.
func approve(w World, wr write) ([]piece, []Change, []claim, *refusal) {
	s, ok := wr.value.(field.Scalar)
	if !ok {
		return nil, nil, nil, refuse("value", "an approval names one proposal")
	}
	hex, err := wr.decl.Kind.Parse(s.Text)
	if err != nil {
		return nil, nil, nil, refuse("value", "%s: %v", wr.path, err)
	}
	proposals := w.State().Proposals()
	ds := make([]events.Digest, len(proposals))
	for i, p := range proposals {
		ds[i] = p.Digest
	}
	d, err := events.ParseDigestPrefix(hex, ds)
	if err != nil {
		return nil, nil, nil, refuse("value", "%s: %v", wr.path, err)
	}
	pr := proposals[slices.Index(ds, d)]
	return []piece{fact{events.Approve{Proposal: d}}}, []Change{Approved{Digest: d, Base: pr.Base.Version()}},
		[]claim{{who: events.Root{}, path: approvalsPath, member: d.String()}}, nil
}

// standing is what became of a proposal once the command's events folded
// into w: the next version in force, or what it still waits on.
func (w World) standing(d events.Digest, base events.PolicyVersion) string {
	if v := w.inForce().Version(); v > base {
		return fmt.Sprintf("⇒ policy version %d in force\n", v)
	}
	for _, pr := range w.State().Proposals() {
		if pr.Digest == d {
			return fmt.Sprintf("⇒ %s %s\n", shortDigest(d), lacks(pr))
		}
	}
	return ""
}
