package events

import (
	"bytes"
	"errors"
	"strings"
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
		if _, err := ReadPropose(wire.NewReader(proposal(n))); !errors.Is(err, errThresholdRange) || !strings.Contains(err.Error(), "from 1 to 255") {
			t.Errorf("a proposal with threshold %d on the wire reads with error %v, want errThresholdRange, saying from 1 to 255", n, err)
		}
	}
	for _, s := range []string{"0", "256"} {
		if _, err := ParseThreshold(s); !errors.Is(err, errThresholdRange) || !strings.Contains(err.Error(), "from 1 to 255") {
			t.Errorf("the threshold %q parses with %v; want errThresholdRange, saying from 1 to 255", s, err)
		}
	}
}

// A majority carries nothing, so one carrying bytes is refused as a
// majority of the wrong length; the majority its writer writes, empty, is
// the control.
func TestAMajorityCarryingBytesIsRefused(t *testing.T) {
	majority := func(b []byte) []byte {
		var w wire.Writer
		w.Bytes(thresholdMajority, b)
		return w.Encoded()
	}
	written := nested(t, payloadBytes(Propose{Base: Digest{3}, Members: map[PrincipalID]Name{}, Threshold: Majority{}}), 3)
	if th, err := readThreshold(majority(nil)); err != nil || th != (Majority{}) || !bytes.Equal(written, majority(nil)) {
		t.Fatalf("the empty majority, as its writer writes it (%x), reads as %#v, %v; the control must read as a majority", written, th, err)
	}
	if th, err := readThreshold(majority([]byte{1})); !errors.Is(err, wrongSize{What: partMajority}) {
		t.Errorf("a majority carrying a byte reads as %#v, %v; want a majority of the wrong length", th, err)
	}
}
