package git

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/go-git/go-billy/v5"
	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/filesystem/dotgit"
	none "github.com/go-git/go-git/v5/storage/memory"
)

// Repository is lit's own object database on disk: a bare git repository
// at its directory, holding lit's objects and refs/lit/**, never the code
// repository's (event-store charter #9). git is its one writer, but for
// the broken refs a listing removes, so git owns its formats, its locks
// and its fsync `[LAW:one-source-of-truth]`;
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
	// repository exists, and opened, the path go-git last opened in it.
	mu     sync.Mutex
	db     *filesystem.Storage
	opened string
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

// pinned is the config lit's repository keeps, set once git init has
// made it in the format go-git reads whole, SHA-1 with refs as files. No
// automatic gc or maintenance, so nothing repacks it under a reader. And
// every object and ref git writes is fsynced to the disk: git's default
// hardens neither a loose object nor a ref, and lit's history is its only
// copy until it is pushed.
var pinned = []string{
	"gc.auto=0",
	"maintenance.auto=false",
	"core.fsync=objects,reference",
	"core.fsyncMethod=fsync",
}

// init makes a repository at dir with the pinned config: its ref format
// given to init itself, the one place it is chosen, and no template, so
// none of the user's hooks is copied in. It holds the empty tree as an
// object, since git holds that tree in every repository without one.
func (d *onDisk) init(dir string) error {
	if _, err := d.g.run("", d.g.litEnv(), nil, "-c", "init.defaultRefFormat=files", "init", "-q", "--bare", "--template=", "--initial-branch=main", "--object-format=sha1", dir); err != nil {
		return err
	}
	for _, kv := range pinned {
		k, v, _ := strings.Cut(kv, "=")
		if _, err := d.git(dir, nil, "config", k, v); err != nil {
			return err
		}
	}
	_, err := d.git(dir, nil, "mktree")
	return err
}

// created says whether the repository has been created, refusing a
// directory at its path that is no repository: git's own mark of one is
// its HEAD, and go-git would read any directory as one.
func (d *onDisk) created() (bool, error) {
	exists, err := d.exists()
	if err != nil || !exists {
		return false, err
	}
	head, err := os.Stat(filepath.Join(d.dir, "HEAD"))
	if err == nil && !head.Mode().IsRegular() {
		err = errors.New("its HEAD is no file")
	}
	if err != nil {
		return false, fmt.Errorf("%s is no git repository: %w", d.dir, err)
	}
	return true, nil
}

// reader is what reads of lit's repository ask of go-git: an object by
// id, whether it holds one, and every ref.
type reader interface {
	EncodedObject(plumbing.ObjectType, plumbing.Hash) (plumbing.EncodedObject, error)
	HasEncodedObject(plumbing.Hash) error
	IterReferences() (storer.ReferenceIter, error)
}

// storage is what reads see: the repository's storage once it
// exists, opened once and shared, or, before then, an empty one, so a read
// of a repository not yet created takes the same steps over nothing
// `[LAW:dataflow-not-control-flow]`. go-git looks for a loose object on
// disk on every read, so it sees each object git writes after opening; it
// reads the list of packs once, so packs that arrive later need a storage
// opened after them.
func (d *onDisk) storage() (reader, error) {
	if d.db != nil {
		return d.db, nil
	}
	created, err := d.created()
	if err != nil {
		return nil, err
	}
	if !created {
		return none.NewStorage(), nil
	}
	// an object is opened lazily, so a look reads its header alone and never
	// inflates a segment to learn its size
	d.db = filesystem.NewStorageWithOptions(unlocked{Filesystem: osfs.New(d.dir), opened: &d.opened}, cache.NewObjectLRUDefault(), filesystem.Options{LargeObjectThreshold: 1})
	return d.db, nil
}

// unlocked is the repository's directory with every file git never reads
// as a ref hidden: a name that begins with a dot, and a lock. git writes a
// ref as <ref>.lock and renames it into place; go-git's ref walk reads
// every file it lists, so a listing during a write would read the
// half-written lock as a ref, and a stray dotfile as a broken one. It
// keeps the path go-git last opened, so a ref file the walk fails on is
// known by name.
type unlocked struct {
	billy.Filesystem
	opened *string
}

func (u unlocked) ReadDir(path string) ([]os.FileInfo, error) {
	fs, err := u.Filesystem.ReadDir(path)
	return slices.DeleteFunc(fs, func(f os.FileInfo) bool {
		return strings.HasPrefix(f.Name(), ".") || strings.HasSuffix(f.Name(), ".lock")
	}), err
}

func (u unlocked) Open(path string) (billy.File, error) {
	*u.opened = path
	return u.Filesystem.Open(path)
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

// write has git store each object's bytes, lit's encoding, as an object of
// its kind, so git, not lit, writes every object file, and checks every
// tree and commit as it would one of its own. The bytes reach git as files
// in a directory of lit's own beside the repository, never in it, one git
// process for each kind objs holds, however many objects that is.
func (d *onDisk) write(objs []object) ([]ObjectID, error) {
	ids := make([]ObjectID, len(objs))
	if len(objs) == 0 {
		return ids, nil
	}
	if err := d.writable(); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(d.dir), ".objects-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	paths := map[kind][]string{}
	at := map[kind][]int{}
	for i, o := range objs {
		k, b := o.encode()
		p := filepath.Join(tmp, strconv.Itoa(i))
		if err := os.WriteFile(p, b, 0o600); err != nil {
			return nil, err
		}
		paths[k], at[k] = append(paths[k], p), append(at[k], i)
	}
	for _, k := range []kind{blobKind, treeKind, commitKind} {
		if len(paths[k]) == 0 {
			continue
		}
		out, err := d.git(d.dir, []byte(strings.Join(paths[k], "\n")+"\n"), "hash-object", "-w", "-t", string(k), "--no-filters", "--stdin-paths")
		if err != nil {
			return nil, err
		}
		lines := strings.Split(out, "\n")
		if len(lines) != len(paths[k]) {
			return nil, fmt.Errorf("git hash-object stored %d %ss; lit gave it %d", len(lines), k, len(paths[k]))
		}
		for j, line := range lines {
			if ids[at[k][j]], err = ParseObjectID(SHA1, line); err != nil {
				return nil, err
			}
		}
	}
	return ids, nil
}

// writable creates the repository if it is not yet, under a git that
// keeps it durable.
func (d *onDisk) writable() error {
	if err := d.g.durable(); err != nil {
		return err
	}
	return d.create()
}

// list is every ref under prefix, read by go-git's walk, with every
// broken ref removed. A ref is broken when it names no object lit's
// repository holds: its file empty, not an id, symbolic, the zero id, or
// torn, none of which git writes, since it writes a ref whole through its
// lock and never at an object it lacks. git neither updates nor deletes a
// broken ref, so lit removes its file, and its writer's next publish makes
// it again from its log; nothing is lost, since the file named nothing.
// Each removal is in the listing, so it is never unseen.
func (d *onDisk) list(prefix RefName) (Listing, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s, err := d.storage()
	if err != nil {
		return Listing{}, err
	}
	removed := []BrokenRef{}
	for {
		heads, broken, err := walk(s, prefix)
		// an empty ref file stops go-git's walk without its name, which is
		// the path go-git opened last; once it is removed the walk is run
		// again
		if errors.Is(err, dotgit.ErrEmptyRefFile) && !slices.ContainsFunc(removed, func(b BrokenRef) bool { return string(b.Name) == filepath.ToSlash(d.opened) }) {
			broken = append(broken, BrokenRef{Name: RefName(filepath.ToSlash(d.opened)), Why: "its file is empty"})
			err = nil
		}
		if err != nil {
			return Listing{}, fmt.Errorf("reading the refs of %s: %w", d.dir, err)
		}
		for _, b := range broken {
			if err := os.Remove(filepath.Join(d.dir, filepath.FromSlash(string(b.Name)))); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return Listing{}, fmt.Errorf("removing %w: %w", b, err)
			}
		}
		removed = append(removed, broken...)
		if heads != nil {
			slices.SortFunc(heads, func(a, b RefHead) int { return strings.Compare(string(a.Name), string(b.Name)) })
			return Listing{Heads: heads, Removed: removed}, nil
		}
	}
}

// walk is every ref under prefix that names an object s holds, and every
// other ref under it, broken; its heads are nil when the walk stopped.
func walk(s reader, prefix RefName) ([]RefHead, []BrokenRef, error) {
	refs, err := s.IterReferences()
	if err != nil {
		return nil, nil, err
	}
	heads, broken := []RefHead{}, []BrokenRef{}
	err = refs.ForEach(func(r *plumbing.Reference) error {
		n := RefName(r.Name())
		if !under(n, prefix) {
			return nil
		}
		if r.Type() != plumbing.HashReference {
			broken = append(broken, BrokenRef{Name: n, Why: "it is symbolic"})
			return nil
		}
		h := r.Hash()
		if err := s.HasEncodedObject(h); errors.Is(err, plumbing.ErrObjectNotFound) {
			broken = append(broken, BrokenRef{Name: n, Why: fmt.Sprintf("it names %s, which lit's repository does not hold", h)})
			return nil
		} else if err != nil {
			return err
		}
		target, err := ObjectIDFrom(SHA1, h[:])
		if err != nil {
			return fmt.Errorf("ref %s: %w", n, err)
		}
		heads = append(heads, RefHead{Name: n, Target: target})
		return nil
	})
	if err != nil {
		return nil, broken, err
	}
	return heads, broken, nil
}

// replace has git move name from what it named to to, refusing if it
// names anything else by then; git's zero id as the old value is a ref
// that must not exist yet.
func (d *onDisk) replace(name RefName, from Was, to ObjectID) error {
	var old string
	switch from := from.(type) {
	case Named:
		old = from.Target.String()
	case Absent:
		old = strings.Repeat("0", 2*SHA1.size())
	default:
		panic(fmt.Sprintf("git: %T is not what a ref was", from))
	}
	if err := d.g.durable(); err != nil {
		return err
	}
	_, err := d.git(d.dir, nil, "update-ref", string(name), to.String(), old)
	return err
}
