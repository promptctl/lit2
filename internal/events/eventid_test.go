package events

import (
	"errors"
	"strings"
	"testing"
)

func TestAnEventIDFileReadsBackTheIDItWasWrittenFrom(t *testing.T) {
	id := EventID{0xab, 1, 2}
	for _, good := range []string{string(id.FileText()), id.String() + "\r\n", id.String()} {
		if got, err := ReadEventIDFile([]byte(good)); err != nil || got != id {
			t.Errorf("ReadEventIDFile(%q) is %s, %v; want %s", good, got, err, id)
		}
	}
	for bad, want := range map[string]error{
		strings.ToUpper(id.String()) + "\n": errNotAnEventID,
		id.String() + "\n\n":                wrongSize{What: partEventIDText},
		id.String()[:62] + "zz\n":           errNotAnEventID,
	} {
		if _, err := ReadEventIDFile([]byte(bad)); !errors.Is(err, want) {
			t.Errorf("ReadEventIDFile(%q) reads with %v; want %v", bad, err, want)
		}
	}
}
