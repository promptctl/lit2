package git

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

// Repository is lit's own object database on disk: a bare git repository
// at its directory, holding lit's objects and refs/lit/**, never the code
// repository's (event-store charter #10). git is its one writer, so git
// owns its formats, its locks and its fsync `[LAW:one-source-of-truth]`;
// go-git reads it in process, so a read costs no process and no pipe round
// trip per object. It is created by its first write, so a read creates
// nothing.
type Repository struct{ repo }

// RepositoryAt is lit's repository at dir, which need not exist yet,
// written by g.
func (g Git) RepositoryAt(dir string) Repository {
	return Repository{repo{b: &onDisk{g: g, dir: dir, at: "--git-dir=" + dir}}}
}

type onDisk struct {
	g   Git
	dir string
	at  string
	// mu guards db, the go-git storage every read shares, opened once the
	// repository exists.
	mu sync.Mutex
	db *filesystem.Storage
}

func (d *onDisk) git(stdin []byte, args ...string) (string, error) {
	out, err := d.g.run("", d.g.litEnv(), stdin, append([]string{d.at}, args...)...)
	return strings.TrimSuffix(string(out), "\n"), err
}

// exists says whether the repository has been created. Before then it
// holds nothing, which every read answers without opening it.
func (d *onDisk) exists() (bool, error) {
	_, err := os.Stat(d.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// create makes the repository, once, as its own step: git init in a
// sibling temporary directory, renamed into place, so a reader never sees
// half a repository. Its format is pinned to the one go-git reads whole:
// SHA-1, refs as files, and no automatic gc or maintenance, so nothing
// repacks it under a reader. It holds the empty tree as an object, since
// git holds that tree in every repository without one. Two racing creations each make one; the
// rename that loses finds the directory there, and whatever it is, every
// git command after reads it as a repository or refuses it.
func (d *onDisk) create() error {
	exists, err := d.exists()
	if err != nil || exists {
		return err
	}
	parent := filepath.Dir(d.dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, ".repo-")
	if err != nil {
		return err
	}
	if err := d.init(tmp); err != nil {
		return errors.Join(err, os.RemoveAll(tmp))
	}
	err = os.Rename(tmp, d.dir)
	if errors.Is(err, fs.ErrExist) {
		return os.RemoveAll(tmp)
	}
	if err != nil {
		return errors.Join(err, os.RemoveAll(tmp))
	}
	return nil
}

func (d *onDisk) init(dir string) error {
	at := "--git-dir=" + dir
	// no template, so none of the user's hooks is copied into lit's repository
	for _, args := range [][]string{
		{"-c", "init.defaultRefFormat=files", "init", "-q", "--bare", "--template=", "--initial-branch=main", "--object-format=sha1", dir},
		{at, "config", "gc.auto", "0"},
		{at, "config", "maintenance.auto", "false"},
		{at, "mktree"},
	} {
		if _, err := d.g.run("", d.g.litEnv(), nil, args...); err != nil {
			return err
		}
	}
	return nil
}

// storage is the go-git storage reads share, or none before the
// repository exists. fresh opens it again, so it sees the packs git
// wrote since: git itself rescans its pack directory on a miss.
func (d *onDisk) storage(fresh bool) (*filesystem.Storage, error) {
	if d.db != nil && !fresh {
		return d.db, nil
	}
	exists, err := d.exists()
	if err != nil || !exists {
		return nil, err
	}
	// git's own mark of a repository; go-git would read any directory as one
	head, err := os.Stat(filepath.Join(d.dir, "HEAD"))
	if err == nil && !head.Mode().IsRegular() {
		err = errors.New("its HEAD is no file")
	}
	if err != nil {
		return nil, fmt.Errorf("%s is no git repository: %w", d.dir, err)
	}
	// an object is opened lazily, so a look reads its header alone and never
	// inflates a segment to learn its size
	d.db = filesystem.NewStorageWithOptions(unlocked{osfs.New(d.dir)}, cache.NewObjectLRUDefault(), filesystem.Options{LargeObjectThreshold: 1})
	return d.db, nil
}

// unlocked is the repository's directory with git's lock files hidden. git
// writes a ref as <ref>.lock and renames it into place, and a lock file is
// never a ref; go-git's ref walk reads every file it lists, so a listing
// during a write would read the half-written lock as a ref.
type unlocked struct{ billy.Filesystem }

func (u unlocked) ReadDir(path string) ([]os.FileInfo, error) {
	fs, err := u.Filesystem.ReadDir(path)
	return slices.DeleteFunc(fs, func(f os.FileInfo) bool { return strings.HasSuffix(f.Name(), ".lock") }), err
}

func (d *onDisk) look(id ObjectID) (kind, uint64, error) {
	o, err := d.object(id)
	if err != nil {
		return "", 0, err
	}
	return kind(o.Type().String()), uint64(o.Size()), nil
}

func (d *onDisk) read(id ObjectID) (kind, []byte, error) {
	o, err := d.object(id)
	if err != nil {
		return "", nil, err
	}
	r, err := o.Reader()
	if err != nil {
		return "", nil, err
	}
	b, err := io.ReadAll(r)
	return kind(o.Type().String()), b, errors.Join(err, r.Close())
}

// object is the object id names, looked for again in a storage opened
// fresh before it is called missing.
func (d *onDisk) object(id ObjectID) (plumbing.EncodedObject, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, fresh := range []bool{false, true} {
		s, err := d.storage(fresh)
		if err != nil {
			return nil, err
		}
		if s == nil {
			break
		}
		o, err := s.EncodedObject(plumbing.AnyObject, plumbing.Hash(id.Sum()))
		if !errors.Is(err, plumbing.ErrObjectNotFound) {
			return o, err
		}
	}
	return nil, MissingObject{ID: id}
}

// write has git build each object from what it holds, a blob from its
// bytes, a tree from its entries and a commit from its tree and message,
// so git, not lit, writes every object file.
func (d *onDisk) write(o object) (ObjectID, error) {
	if err := d.create(); err != nil {
		return ObjectID{}, err
	}
	var out string
	var err error
	switch o := o.(type) {
	case blob:
		out, err = d.git(o, "hash-object", "-w", "--stdin")
	case tree:
		var in []byte
		for _, e := range o {
			in = append(in, "100644 blob "+e.Blob.String()+"\t"+e.Name+"\x00"...)
		}
		out, err = d.git(in, "mktree", "-z")
	case commit:
		out, err = d.git([]byte(o.Message), "commit-tree", "--no-gpg-sign", o.Tree.String())
	}
	if err != nil {
		return ObjectID{}, err
	}
	return ParseObjectID(SHA1, out)
}

// list is every ref under prefix, sorted by name as git sorts them.
func (d *onDisk) list(prefix RefName) ([]RefHead, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	heads := []RefHead{}
	s, err := d.storage(false)
	if err != nil || s == nil {
		return heads, err
	}
	refs, err := s.IterReferences()
	if err != nil {
		return nil, fmt.Errorf("reading the refs of %s: %w", d.dir, err)
	}
	// go-git reads a ref file that is not an id as the null id, or as a
	// symbolic ref; git writes neither under lit's refs, so either is a
	// broken file, refused by name `[LAW:no-silent-failure]`
	err = refs.ForEach(func(r *plumbing.Reference) error {
		n := RefName(r.Name())
		switch {
		case !under(n, prefix):
			return nil
		case r.Type() != plumbing.HashReference || r.Hash().IsZero():
			return fmt.Errorf("ref %s names no object: %s", n, r.Strings()[1])
		}
		h := r.Hash()
		target, err := ObjectIDFrom(SHA1, h[:])
		if err != nil {
			return fmt.Errorf("ref %s: %w", n, err)
		}
		heads = append(heads, RefHead{Name: n, Target: target})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reading the refs of %s: %w", d.dir, err)
	}
	slices.SortFunc(heads, func(a, b RefHead) int { return strings.Compare(string(a.Name), string(b.Name)) })
	return heads, nil
}

func (d *onDisk) replace(name RefName, to ObjectID) error {
	_, err := d.git(nil, "update-ref", string(name), to.String())
	return err
}
