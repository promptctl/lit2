package git

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// objectDB is Objects over a go-git object storer. The adapter and the twin
// are this one type and differ only in how a storer is reached
// `[LAW:one-type-per-behavior]`: reach hands over the storer and the
// function that releases it, creating the repository first when create is
// set, since nothing is created by a read.
type objectDB struct {
	reach func(create bool) (storer.EncodedObjectStorer, func(), error)
}

// identity is every lit commit's author and committer: fixed, at time zero,
// so a commit is a pure function of its tree and message, publish reads no
// clock, and racing publishes of one log converge on one id
// `[LAW:effects-at-boundaries]`.
var identity = object.Signature{Name: "lit", Email: "lit", When: time.Unix(0, 0).UTC()}

func (db objectDB) PutBlob(b []byte) (ObjectID, Stored, error) {
	return db.put(plumbing.BlobObject, b)
}

func (db objectDB) Blob(id ObjectID) ([]byte, error) {
	return db.content(plumbing.BlobObject, id)
}

func (db objectDB) BlobSize(id ObjectID) (uint64, error) {
	h, err := id.hash()
	if err != nil {
		return 0, err
	}
	s, release, err := db.reach(false)
	if err != nil {
		return 0, err
	}
	defer release()
	size, err := s.EncodedObjectSize(h)
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return 0, MissingObject{ID: id}
	}
	if err != nil {
		return 0, err
	}
	return uint64(size), nil
}

// PutTree stores es as a tree, sorted by name as git requires. A name git
// would refuse, or two entries of one name, is refused here.
func (db objectDB) PutTree(es []Entry) (ObjectID, Stored, error) {
	sorted := slices.SortedFunc(slices.Values(es), func(a, b Entry) int { return strings.Compare(a.Name, b.Name) })
	t := object.Tree{Entries: make([]object.TreeEntry, len(sorted))}
	for i, e := range sorted {
		if e.Name == "" || e.Name == "." || e.Name == ".." || strings.ContainsAny(e.Name, "/\x00") {
			return ObjectID{}, 0, fmt.Errorf("%q cannot name a tree entry", e.Name)
		}
		if i > 0 && sorted[i-1].Name == e.Name {
			return ObjectID{}, 0, fmt.Errorf("two tree entries are named %q", e.Name)
		}
		h, err := e.Blob.hash()
		if err != nil {
			return ObjectID{}, 0, err
		}
		t.Entries[i] = object.TreeEntry{Name: e.Name, Mode: filemode.Regular, Hash: h}
	}
	o := &plumbing.MemoryObject{}
	if err := t.Encode(o); err != nil {
		return ObjectID{}, 0, err
	}
	return db.store(o)
}

// Tree reads a lit tree: every entry a regular file. Any other entry is a
// tree lit did not write, refused rather than read as less than it is.
func (db objectDB) Tree(id ObjectID) ([]Entry, error) {
	var t object.Tree
	if err := db.decode(plumbing.TreeObject, id, t.Decode); err != nil {
		return nil, err
	}
	es := make([]Entry, len(t.Entries))
	for i, e := range t.Entries {
		if e.Mode != filemode.Regular {
			return nil, fmt.Errorf("tree %s holds %q of mode %s: not a lit tree", id, e.Name, e.Mode)
		}
		es[i] = Entry{Name: e.Name, Blob: sha1ID(e.Hash)}
	}
	return es, nil
}

func (db objectDB) PutCommit(c Commit) (ObjectID, Stored, error) {
	tree, err := c.Tree.hash()
	if err != nil {
		return ObjectID{}, 0, err
	}
	commit := object.Commit{Author: identity, Committer: identity, Message: c.Message, TreeHash: tree}
	o := &plumbing.MemoryObject{}
	if err := commit.Encode(o); err != nil {
		return ObjectID{}, 0, err
	}
	return db.store(o)
}

// Commit reads a lit commit. A commit with parents carries history, which
// no lit commit does, so it is refused.
func (db objectDB) Commit(id ObjectID) (Commit, error) {
	var c object.Commit
	if err := db.decode(plumbing.CommitObject, id, c.Decode); err != nil {
		return Commit{}, err
	}
	if len(c.ParentHashes) != 0 {
		return Commit{}, fmt.Errorf("commit %s has %d parents: not a lit commit", id, len(c.ParentHashes))
	}
	return Commit{Tree: sha1ID(c.TreeHash), Message: c.Message}, nil
}

func (db objectDB) put(t plumbing.ObjectType, b []byte) (ObjectID, Stored, error) {
	o := &plumbing.MemoryObject{}
	o.SetType(t)
	if _, err := o.Write(b); err != nil {
		return ObjectID{}, 0, err
	}
	return db.store(o)
}

// store adds o unless the database holds it already; content addressing
// makes a second write of one object the same object.
func (db objectDB) store(o *plumbing.MemoryObject) (ObjectID, Stored, error) {
	s, release, err := db.reach(true)
	if err != nil {
		return ObjectID{}, 0, err
	}
	defer release()
	id := sha1ID(o.Hash())
	err = s.HasEncodedObject(o.Hash())
	if err == nil {
		return id, Existing, nil
	}
	if !errors.Is(err, plumbing.ErrObjectNotFound) {
		return ObjectID{}, 0, err
	}
	if _, err := s.SetEncodedObject(o); err != nil {
		return ObjectID{}, 0, err
	}
	return id, New, nil
}

func (db objectDB) object(t plumbing.ObjectType, id ObjectID) (plumbing.EncodedObject, error) {
	h, err := id.hash()
	if err != nil {
		return nil, err
	}
	s, release, err := db.reach(false)
	if err != nil {
		return nil, err
	}
	defer release()
	o, err := s.EncodedObject(plumbing.AnyObject, h)
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return nil, MissingObject{ID: id}
	}
	if err != nil {
		return nil, err
	}
	if o.Type() != t {
		return nil, fmt.Errorf("object %s is a %s, not a %s", id, o.Type(), t)
	}
	return o, nil
}

func (db objectDB) content(t plumbing.ObjectType, id ObjectID) (b []byte, err error) {
	o, err := db.object(t, id)
	if err != nil {
		return nil, err
	}
	r, err := o.Reader()
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, r.Close()) }()
	return io.ReadAll(r)
}

func (db objectDB) decode(t plumbing.ObjectType, id ObjectID, decode func(plumbing.EncodedObject) error) error {
	o, err := db.object(t, id)
	if err != nil {
		return err
	}
	return decode(o)
}

// sha1ID is a go-git hash as an id in lit's repository.
func sha1ID(h plumbing.Hash) ObjectID {
	id := ObjectID{format: SHA1}
	copy(id.sum[:], h[:])
	return id
}

// hash is the id as go-git names it: lit's repository is SHA-1, so an id of
// another format, or the zero id, names nothing in it.
func (id ObjectID) hash() (plumbing.Hash, error) {
	switch id.format {
	case SHA1:
		var h plumbing.Hash
		copy(h[:], id.sum[:len(h)])
		return h, nil
	case SHA256:
	}
	return plumbing.Hash{}, ForeignFormat{ID: id}
}
