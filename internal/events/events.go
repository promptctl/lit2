// Package events is the event record and what travels in it.
//
// Its layer in architecture.md §6: record, codec, upcasters, ids,
// signature, envelope (§3.2, §3.14, §3.17, §3.19); every signature and
// envelope names its crypto suite, one value behind one interface here (§8).
// Decode turns one record of the log into an Event, keeping every field it
// does not know (INV:preserve-unknown); Mint signs a command's drafts as
// events at the next causal positions and names what they create; Compare
// is the total order the fold sorts by. Envelope holds one prose value,
// opaque below the verbs; Suites judges a signature against its principal;
// Key is a principal's private key in its file form and its recovery
// phrase. Every function here is pure: entropy and the clock arrive as
// values from the root `[LAW:effects-at-boundaries]`.
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
