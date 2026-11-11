// Package events is the event record and what travels in it.
//
// Its layer in architecture.md §6: record, codec, upcasters, ids,
// signature, envelope (§3.2, §3.14, §3.17, §3.19); every signature and
// envelope names its crypto suite, one value behind one interface here (§8).
// Here it holds the agent label, the third part of every event's
// attribution (checkout, principal, agent).
package events

// Agent is the label the invoking harness supplies, or its absence.
//
//sumtype:decl
type Agent interface{ agent() }

// Labeled is an agent label: an opaque string, shown beside comments and
// history, never interpreted (§3.14).
type Labeled struct{ label string }

// Unlabeled is an invocation whose harness supplied no label.
type Unlabeled struct{}

func (Labeled) agent()   {}
func (Unlabeled) agent() {}

// String is the label as the harness supplied it.
func (l Labeled) String() string { return l.label }

// AgentFrom reads the label from LIT_AGENT's value (D7): empty is Unlabeled
// and anything else is the label, kept opaque, so the read cannot fail.
func AgentFrom(env string) Agent {
	if env == "" {
		return Unlabeled{}
	}
	return Labeled{label: env}
}
