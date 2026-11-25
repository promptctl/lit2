package git_test

import (
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/promptctl/lit2/internal/git"
)

// TestHashBlobEmpty pins the id git gives the empty blob in SHA-1, the one
// every reader of a missing file's content is compared against.
func TestHashBlobEmpty(t *testing.T) {
	const sha1Empty = "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391"
	if got := git.HashBlob(git.SHA1, nil).String(); got != sha1Empty {
		t.Errorf("SHA-1 empty blob = %s, want %s", got, sha1Empty)
	}
}

func TestTwoIDsOfOneSumInTwoFormatsDiffer(t *testing.T) {
	const sum = "ce013625030ba8dba906f756967f9e9ca394464a"
	sha1, err := git.ParseObjectID(git.SHA1, sum)
	if err != nil {
		t.Fatal(err)
	}
	sha256, err := git.ParseObjectID(git.SHA256, sum+"000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if sha1 == sha256 {
		t.Errorf("a sha1 id and a sha256 id of one sum compare equal")
	}
	if short, err := git.ParseObjectID(git.SHA256, sum); !errors.Is(err, git.ErrSumSize) {
		t.Errorf("a sha1 sum read as a sha256 id = %v, %v; want ErrSumSize", short, err)
	}
	if got := hex.EncodeToString(sha1.Sum()); got != sum {
		t.Errorf("a sha1 id's sum is %s; want %s", got, sum)
	}
	if got := hex.EncodeToString(sha256.Sum()); got != sum+"000000000000000000000000" {
		t.Errorf("a sha256 id's sum is %s; want its 32 bytes", got)
	}
	for _, id := range []git.ObjectID{sha1, sha256} {
		if back, err := git.ObjectIDFrom(id.Format(), id.Sum()); err != nil || back != id {
			t.Errorf("ObjectIDFrom(%v, Sum) = %v, %v; want %v", id.Format(), back, err, id)
		}
	}
	// the round trips above are the control: each sum read in its own
	// format is an id
	for name, c := range map[string]struct {
		read func() (git.ObjectID, error)
		want error
	}{
		"a sha1 sum as sha256": {func() (git.ObjectID, error) { return git.ObjectIDFrom(git.SHA256, sha1.Sum()) }, git.ErrSumSize},
		"a sha256 sum as sha1": {func() (git.ObjectID, error) { return git.ObjectIDFrom(git.SHA1, sha256.Sum()) }, git.ErrSumSize},
		"no format":            {func() (git.ObjectID, error) { return git.ObjectIDFrom(0, sha1.Sum()) }, git.ErrNoFormat},
		"an empty sum":         {func() (git.ObjectID, error) { return git.ObjectIDFrom(git.SHA1, nil) }, git.ErrSumSize},
	} {
		if id, err := c.read(); !errors.Is(err, c.want) {
			t.Errorf("%s reads as %v, %v; want %v", name, id, err, c.want)
		}
	}
}

// The zero id names no object: it prints as such, and no format reads one.
func TestTheZeroIDPrintsAsNoIDAndNoFormatReadsOne(t *testing.T) {
	if got := fmt.Sprintf("%v %+v", git.ObjectID{}, git.ObjectID{}.Format()); got != "(no object id) no format (0)" {
		t.Errorf("the zero id prints as %q", got)
	}
	const sum = "ce013625030ba8dba906f756967f9e9ca394464a"
	if _, err := git.ParseObjectID(git.SHA1, sum); err != nil {
		t.Errorf("the id read in its format failed with %v", err)
	}
	if id, err := git.ParseObjectID(0, sum); !errors.Is(err, git.ErrNoFormat) {
		t.Errorf("an id read in no format = %v, %v; want ErrNoFormat", id, err)
	}
}
