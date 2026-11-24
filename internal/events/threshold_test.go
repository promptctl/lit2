package events

import (
	"errors"
	"testing"

	"github.com/promptctl/lit2/internal/events/wire"
)

func TestAWireThresholdOutsideOneTo255IsRefusedAsTheTextOneIs(t *testing.T) {
	proposal := func(n uint64) []byte {
		var th, w wire.Writer
		th.Uvarint(thresholdFixed, n)
		w.Bytes(1, make([]byte, 32))
		w.Bytes(3, th.Encoded())
		return w.Encoded()
	}
	// the controls: the ends of the range read, on the wire and as text
	for _, n := range []uint64{1, 255} {
		if _, err := ReadPropose(wire.NewReader(proposal(n))); err != nil {
			t.Fatalf("a proposal with threshold %d: %v; the control must read", n, err)
		}
	}
	for _, s := range []string{"1", "255", "majority"} {
		if _, err := ParseThreshold(s); err != nil {
			t.Fatalf("the threshold %q: %v; the control must parse", s, err)
		}
	}
	for _, n := range []uint64{0, 256} {
		if _, err := ReadPropose(wire.NewReader(proposal(n))); !errors.Is(err, errThresholdRange) {
			t.Errorf("a proposal with threshold %d on the wire reads with error %v, want errThresholdRange", n, err)
		}
	}
	for _, s := range []string{"0", "256"} {
		if _, err := ParseThreshold(s); !errors.Is(err, errThresholdRange) {
			t.Errorf("the threshold %q parses with %v; want errThresholdRange", s, err)
		}
	}
}
