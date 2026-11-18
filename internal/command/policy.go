package command

import (
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/fold"
)

// PolicyFacts is the policy a run's store was folded under and what the
// fold ruled on each event the run minted `[LAW:nothing-unseen]`: Under is
// nil until a store is folded, Bootstrap is admin init's step and empty on
// every other run, and Verdicts is empty on a run that minted nothing.
type PolicyFacts struct {
	Under     fold.PolicyInForce
	Bootstrap string
	Verdicts  []Verdict
}

// Verdict is the fold's outcome on one minted event.
type Verdict struct {
	Event   events.EventID
	Outcome fold.Outcome
}

// Verdicts pairs each minted event with its outcome, the two in the order
// the fold ruled them.
func Verdicts(minted []events.Event, outcomes []fold.Outcome) []Verdict {
	out := make([]Verdict, len(minted))
	for i, e := range minted {
		out[i] = Verdict{Event: e.ID(), Outcome: outcomes[i]}
	}
	return out
}

type (
	policyLine struct {
		Under     underLine     `json:"under"`
		Bootstrap string        `json:"bootstrap,omitempty"`
		Verdicts  []verdictLine `json:"verdicts"`
	}
	underLine struct {
		State   string `json:"state"`
		Version uint64 `json:"version,omitempty"`
	}
	verdictLine struct {
		Event    string     `json:"event"`
		Outcome  string     `json:"outcome"`
		Under    uint64     `json:"under,omitempty"`
		Grant    string     `json:"grant,omitempty"`
		Reason   string     `json:"reason,omitempty"`
		Standing string     `json:"standing,omitempty"`
		Misses   []missLine `json:"misses,omitempty"`
	}
	missLine struct {
		Capability string   `json:"capability"`
		Failed     []string `json:"failed"`
	}
)

// policyBlock is the facts as the line's policy block, absent when no
// store was folded. An unenrolled store is its own state, never version 0.
func policyBlock(p PolicyFacts) *policyLine {
	if p.Under == nil {
		return nil
	}
	l := &policyLine{Bootstrap: p.Bootstrap, Verdicts: []verdictLine{}}
	switch u := p.Under.(type) {
	case fold.Unenrolled:
		l.Under = underLine{State: "unenrolled"}
	case fold.FoundedUnread:
		l.Under = underLine{State: "founded_unread"}
	case fold.Enrolled:
		l.Under = underLine{State: "enrolled", Version: uint64(u.Policy.Version())}
	}
	for _, v := range p.Verdicts {
		l.Verdicts = append(l.Verdicts, verdictOf(v))
	}
	return l
}

func verdictOf(v Verdict) verdictLine {
	l := verdictLine{Event: v.Event.String()}
	switch o := v.Outcome.(type) {
	case fold.Applied:
		l.Outcome, l.Under, l.Grant = "applied", uint64(o.Under), grantName(o.Grant)
	case fold.CarriedFor:
		l.Outcome, l.Reason = "carried", fold.ReasonName(o.Why)
		if u, ok := o.Why.(fold.Unauthorized); ok {
			l.Standing = standingName(u.Refused.As)
			for _, m := range u.Refused.Misses {
				l.Misses = append(l.Misses, missOf(m))
			}
		}
	case fold.LeftFor:
		l.Outcome, l.Reason = "unapplied", fold.WhyName(o.Why)
	}
	return l
}

func grantName(g fold.Grant) string {
	switch g := g.(type) {
	case fold.OwnerSurface:
		return "owner"
	case fold.ByCapability:
		return g.Capability.String()
	case fold.ByPolicyRule:
		return "policy_rule"
	}
	panic("command: a grant with no name")
}

func standingName(s fold.Standing) string {
	switch s := s.(type) {
	case fold.AsRole:
		return s.Role.String()
	case fold.NotAMember:
		return "not_a_member"
	}
	panic("command: a standing with no name")
}

func missOf(m fold.Miss) missLine {
	l := missLine{Capability: m.Capability.String(), Failed: []string{}}
	for _, f := range m.Failed {
		switch f.(type) {
		case fold.PathOutside:
			l.Failed = append(l.Failed, "path_outside")
		case fold.BindingUnmet:
			l.Failed = append(l.Failed, "binding_unmet")
		case fold.ConditionUnmet:
			l.Failed = append(l.Failed, "condition_unmet")
		}
	}
	return l
}
