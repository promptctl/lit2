package events

import (
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
	for _, bad := range []string{
		strings.ToUpper(id.String()) + "\n",
		id.String() + "\n\n",
		id.String()[:62] + "zz\n",
	} {
		if _, err := ReadEventIDFile([]byte(bad)); err == nil {
			t.Errorf("ReadEventIDFile(%q) read an id", bad)
		}
	}
}
