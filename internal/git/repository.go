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
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/filesystem"
	none "github.com/go-git/go-git/v5/storage/memory"
)

// Repository is lit's own object database on disk: a bare git repository
// at its directory, holding lit's objects and refs/lit/**, never the code
// repository's (event-store charter #9). git is its one writer, so git
// owns its formats, its locks and its fsync `[LAW:one-source-of-truth]`;
// go-git reads it in process, so a read costs no process and no pipe round
// trip per object. It is created by its first write, so a read creates
// nothing.
type Repository struct{ repo }

// RepositoryAt is lit's repository at dir, which need not exist yet,
// written by g.
func (g *Git) RepositoryAt(dir string) Repository {
	return Repository{repo{b: &onDisk{g: g, dir: dir}}}
}

type onDisk struct {
	g   *Git
	dir string
	// mu guards db, the go-git storage every read shares, opened once the
	// repository exists.
	mu sync.Mutex
	db *filesystem.Storage
}

// git runs git on the repository at dir: the one place a spawn names
// which repository it is for.
func (d *onDisk) git(dir string, stdin []byte, args ...string) (string, error) {
	out, err := d.g.run("", d.g.litEnv(), stdin, append([]string{"--git-dir=" + dir}, args...)...)
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
// half a repository. Two racing creations each make one; the rename that
// loses finds the directory there, and whatever it is, every git command
// after reads it as a repository or refuses it.
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

// pinned is the config lit's repository is created with. SHA-1 and refs
// as files are the format go-git reads whole. No automatic gc or
// maintenance, so nothing repacks it under a reader. And every object and
// ref git writes is fsynced to the disk: git's default hardens neither a
// loose object nor a ref, and lit's history is its only copy until it is
// pushed. No template is copied, so none of the user's hooks is either.
var pinned = []string{
	"init.defaultRefFormat=files",
	"gc.auto=0",
	"maintenance.auto=false",
	"core.fsync=objects,reference",
	"core.fsyncMethod=fsync",
}

// floor is the oldest git that honors the pinned config: git before 2.36
// takes core.fsync without a word and fsyncs no ref, so lit's repository
// would be created less durable than it says.
var floor = [2]int{2, 36}

// atFloor refuses a git whose git version says it is older than floor.
func atFloor(version string) error {
	var v [2]int
	if _, err := fmt.Sscanf(version, "git version %d.%d", &v[0], &v[1]); err != nil || v[0] < floor[0] || v[0] == floor[0] && v[1] < floor[1] {
		return fmt.Errorf("lit2 needs git %d.%d or later to keep its repository durable; %q is what git says it is", floor[0], floor[1], strings.TrimSpace(version))
	}
	return nil
}

// init makes a repository at dir with the pinned config, refusing a git
// older than floor. It holds the empty tree as an object, since git holds
// that tree in every repository without one.
func (d *onDisk) init(dir string) error {
	out, err := d.g.run("", d.g.litEnv(), nil, "version")
	if err != nil {
		return err
	}
	if err := atFloor(string(out)); err != nil {
		return err
	}
	if _, err := d.g.run("", d.g.litEnv(), nil, "init", "-q", "--bare", "--template=", "--initial-branch=main", "--object-format=sha1", dir); err != nil {
		return err
	}
	for _, kv := range pinned {
		k, v, _ := strings.Cut(kv, "=")
		if _, err := d.git(dir, nil, "config", k, v); err != nil {
			return err
		}
	}
	_, err = d.git(dir, nil, "mktree")
	return err
}

// reader is what reads of lit's repository ask of go-git: an object by
// id, and every ref.
type reader interface {
	EncodedObject(plumbing.ObjectType, plumbing.Hash) (plumbing.EncodedObject, error)
	IterReferences() (storer.ReferenceIter, error)
}

// storage is what reads see: the repository's storage once it exists,
// opened once and shared, or, before then, an empty one, so a read of a
// repository not yet created takes the same steps over nothing
// `[LAW:dataflow-not-control-flow]`. go-git looks for a loose object on
// disk on every read, so it sees each object git writes after opening; it
// reads the list of packs once, so packs that arrive later need a storage
// opened after them.
func (d *onDisk) storage() (reader, error) {
	if d.db != nil {
		return d.db, nil
	}
	exists, err := d.exists()
	if err != nil {
		return nil, err
	}
	if !exists {
		return none.NewStorage(), nil
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
	o, k, err := d.object(id)
	if err != nil {
		return "", 0, err
	}
	return k, uint64(o.Size()), nil
}

func (d *onDisk) read(id ObjectID) (kind, []byte, error) {
	o, k, err := d.object(id)
	if err != nil {
		return "", nil, err
	}
	r, err := o.Reader()
	if err != nil {
		return "", nil, err
	}
	b, err := io.ReadAll(r)
	return k, b, errors.Join(err, r.Close())
}

// object is the object id names and its kind, or MissingObject.
func (d *onDisk) object(id ObjectID) (plumbing.EncodedObject, kind, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s, err := d.storage()
	if err != nil {
		return nil, "", err
	}
	o, err := s.EncodedObject(plumbing.AnyObject, plumbing.Hash(id.Sum()))
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return nil, "", MissingObject{ID: id}
	}
	if err != nil {
		return nil, "", err
	}
	k, ok := kinds[o.Type()]
	if !ok {
		return nil, "", fmt.Errorf("object %s is a %s, which lit's repository never holds", id, o.Type())
	}
	return o, k, nil
}

// kinds is each of go-git's object types that is one of lit's kinds.
var kinds = map[plumbing.ObjectType]kind{
	plumbing.BlobObject:   blobKind,
	plumbing.TreeObject:   treeKind,
	plumbing.CommitObject: commitKind,
}

// write has git store o's bytes, lit's encoding, as an object of its
// kind, so git, not lit, writes every object file, and checks every tree
// and commit as it would one of its own.
func (d *onDisk) write(o object) (ObjectID, error) {
	if err := d.create(); err != nil {
		return ObjectID{}, err
	}
	k, b := o.encode()
	out, err := d.git(d.dir, b, "hash-object", "-w", "-t", string(k), "--stdin")
	if err != nil {
		return ObjectID{}, err
	}
	return ParseObjectID(SHA1, out)
}

// list is every ref under prefix, sorted by name as git sorts them.
func (d *onDisk) list(prefix RefName) ([]RefHead, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s, err := d.storage()
	if err != nil {
		return nil, err
	}
	refs, err := s.IterReferences()
	if err != nil {
		return nil, fmt.Errorf("reading the refs of %s: %w", d.dir, err)
	}
	heads := []RefHead{}
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

// replace has git move name from what it named to to, refusing if it
// names anything else by then; git's zero id as the old value is a ref
// that must not exist yet.
func (d *onDisk) replace(name RefName, from Was, to ObjectID) error {
	old := strings.Repeat("0", 2*SHA1.size())
	if n, ok := from.(Named); ok {
		old = n.Target.String()
	}
	_, err := d.git(d.dir, nil, "update-ref", string(name), to.String(), old)
	return err
}
