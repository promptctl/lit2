package events

import (
	"strings"
	"testing"
)

func TestAnEventIDFileReadsBackTheIDItWasWrittenFrom(t *testing.T) {
	id := EventID{0xab, 1, 2}
	if got, err := ReadEventIDFile(id.FileText()); err != nil || got != id {
		t.Errorf("ReadEventIDFile(FileText) is %s, %v; want %s", got, err, id)
	}
	for _, bad := range []string{
		strings.ToUpper(id.String()) + "\n",
		id.String(),
		id.String()[:62] + "zz\n",
	} {
		if _, err := ReadEventIDFile([]byte(bad)); err == nil {
			t.Errorf("ReadEventIDFile(%q) read an id", bad)
		}
	}
}
