package store

import (
	"bytes"
	"strings"
	"testing"
)

func TestACheckoutIDHasOneSpelling(t *testing.T) {
	const s = "7a1e0f7b112d4c889e015a6b3c2f4099"
	id, err := ParseCheckoutID(s)
	if err != nil || id.String() != s {
		t.Errorf("ParseCheckoutID(%q) = %v, %v; want it back", s, id, err)
	}
	for _, bad := range []string{"", "7A1E0F7B112D4C889E015A6B3C2F4099", s[:31], s + "00", "7a1e0f7b112d4c889e015a6b3c2f409g", s + "\n"} {
		if _, err := ParseCheckoutID(bad); err == nil {
			t.Errorf("ParseCheckoutID(%q) read an id", bad)
		}
	}
}

func TestTheIDFileFormReadsBackWhatItWrites(t *testing.T) {
	const s = "7a1e0f7b112d4c889e015a6b3c2f4099"
	text := []byte(s + "\n")
	id, err := ReadCheckoutIDFile(text)
	if err != nil || !bytes.Equal(id.FileText(), text) {
		t.Errorf("FileText(ReadCheckoutIDFile(%q)) = %q, %v; want it back", text, id.FileText(), err)
	}
	if back, err := ReadCheckoutIDFile(id.FileText()); err != nil || back != id {
		t.Errorf("ReadCheckoutIDFile(FileText()) = %v, %v; want %v", back, err, id)
	}
	for _, bad := range []string{s, s + "\n\n", s + "\r\n", " " + s + "\n", "\n", strings.Repeat("hunter2", 999), strings.Repeat("hunter2", 4) + "zzzz\n"} {
		if _, err := ReadCheckoutIDFile([]byte(bad)); err == nil || strings.Contains(err.Error(), "hunter2") {
			t.Errorf("ReadCheckoutIDFile(%q) = %v; want an error that does not carry the file", bad, err)
		}
	}
}
