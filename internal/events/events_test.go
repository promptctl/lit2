package events

import "testing"

func TestTheAgentLabelIsOpaque(t *testing.T) {
	if got := AgentFrom(""); got != (Unlabeled{}) {
		t.Errorf("an empty LIT_AGENT is %#v; want Unlabeled", got)
	}
	for _, label := range []string{"claude", " spaced out ", "a/b=c"} {
		if got, ok := AgentFrom(label).(Labeled); !ok || got.String() != label {
			t.Errorf("LIT_AGENT=%q is %#v; want that label, as given", label, AgentFrom(label))
		}
	}
}
