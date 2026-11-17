package events

import (
	"strings"
	"testing"

	"github.com/promptctl/lit2/internal/events/wire"
)

func TestAWireThresholdOutsideOneTo255IsRefusedAsTheTextOneIs(t *testing.T) {
	for _, n := range []uint64{0, 256} {
		var th, w wire.Writer
		th.Uvarint(thresholdFixed, n)
		w.Uvarint(1, 1)
		w.Bytes(3, th.Encoded())
		if _, err := ReadPropose(wire.NewReader(w.Encoded())); err == nil || !strings.Contains(err.Error(), "from 1 to 255") {
			t.Errorf("a proposal with threshold %d on the wire reads with error %v, want it refused", n, err)
		}
	}
	for _, s := range []string{"0", "256"} {
		if _, err := ParseThreshold(s); err == nil {
			t.Errorf("the threshold %q parses", s)
		}
	}
}
