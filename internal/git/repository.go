package git

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/storage/filesystem"
	none "github.com/go-git/go-git/v5/storage/memory"

	"github.com/promptctl/lit2/internal/files"
)

// Repository is lit's own object database on disk: a bare git repository
// at its directory, holding lit's objects and refs/lit/**, never the code
// repository's (event-store charter #9). git is its one writer, so git
// owns its formats, its locks
// and its fsync `[LAW:one-source-of-truth]`;
// go-git reads it in process, so a read costs no process and no pipe round
// trip per object. It is created by its first write, so a read creates
// nothing.
type Repository struct{ repo }

// RepositoryAt is lit's repository, the directory at, which need not
// exist yet, written by g, the syncs of its entry kept by u, the
// process's, which its directories' other ports share.
func (g *Git) RepositoryAt(at files.Dir, u *files.Durability) Repository {
	return Repository{repo{b: &onDisk{g: g, at: at, entry: u}}}
}

type onDisk struct {
	g *Git
	// at is the repository's directory, its one location: every path git
	// or go-git is given comes from path
	at files.Dir
	// mu guards db, the go-git storage every read shares, opened once the
	// repository exists, and packs, when its pack directory last changed as
	// db read it.
	mu    sync.Mutex
	db    *filesystem.Storage
	packs time.Time
	// entry is the process's keeper of the syncs of the repository's entry
	// and the directories on its way: once one of a directory fails, every
	// later one of it answers that failure, whichever port asks
	entry *files.Durability
}

// git runs git on the repository at dir: the one place a spawn names
// which repository it is for.
func (d *onDisk) git(dir string, stdin []byte, args ...string) (string, error) {
	out, err := d.g.run("", d.g.litEnv(), stdin, append([]string{"--git-dir=" + dir}, args...)...)
	return strings.TrimSuffix(string(out), "\n"), err
}

// path is the repository's directory as git and go-git take it, once the
// files step has checked every element of it from its base, the
// repository's own directory among them, by Lstat: one that is a symlink,
// dangling or not, or no directory is NotTheDirectory, and one missing is
// fs.ErrNotExist, beside the path, at which a read then finds nothing.
// git takes paths, not descriptors, so the directory is checked, then
// named by its path; this is the one place every use of it starts
// `[LAW:single-enforcer]`.
func (d *onDisk) path() (string, error) {
	return d.at.Path(), d.at.Check()
}

// create makes the repository, once, as its own step: git init in a
// sibling temporary directory, renamed into place, so a reader never sees
// half a repository. Two racing creations each make one; the rename that
// loses finds the directory there, and whatever it is, every git command
// after reads it as a repository or refuses it.
func (d *onDisk) create() error {
	// a directory there is created; anything else at its name, a link to
	// nothing among them, is refused, never renamed over
	_, err := d.path()
	if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	in := d.at.Parent()
	if err := d.entry.Make(in); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(in.Path(), ".repo-")
	if err != nil {
		return err
	}
	if err := d.init(tmp); err != nil {
		return errors.Join(err, os.RemoveAll(tmp))
	}
	err = os.Rename(tmp, d.at.Path())
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

// created is the repository's path, and whether it has been created,
// refusing a directory at its path that is no repository: git's own mark
// of one is its HEAD, and go-git would read any directory as one. Before
// it is created it holds nothing, which every read answers without
// opening it.
func (d *onDisk) created() (string, bool, error) {
	p, err := d.path()
	if errors.Is(err, fs.ErrNotExist) {
		return p, false, nil
	}
	if err != nil {
		return "", false, err
	}
	head, err := os.Lstat(filepath.Join(p, "HEAD"))
	if err == nil && !head.Mode().IsRegular() {
		err = errors.New("its HEAD is no file")
	}
	if err != nil {
		return "", false, fmt.Errorf("%s: %w: %w", p, errNoRepository, err)
	}
	return p, true, nil
}

// errNoRepository is a directory at the repository's path that is not one.
var errNoRepository = errors.New("no git repository")

// reader is what reads of lit's repository ask of go-git: an object by
// id, and a ref by name.
type reader interface {
	EncodedObject(plumbing.ObjectType, plumbing.Hash) (plumbing.EncodedObject, error)
	Reference(plumbing.ReferenceName) (*plumbing.Reference, error)
}

// storage is what reads see: the repository's storage once it
// exists, opened once and shared, or, before then, an empty one, so a read
// of a repository not yet created takes the same steps over nothing
// `[LAW:dataflow-not-control-flow]`. go-git looks for a loose object on
// disk on every read, so it sees each object git writes after opening; it
// reads the list of packs once, so packs that arrive later need a storage
// opened after them.
//
// Every read checks the repository's directory again, so a repo/ swapped
// for a link after go-git opened it is refused, not read through.
func (d *onDisk) storage() (reader, string, error) {
	p, created, err := d.created()
	if err != nil {
		return nil, "", err
	}
	if !created {
		return none.NewStorage(), p, nil
	}
	if d.db == nil {
		// an object is opened lazily, so a look reads its header alone and
		// never inflates a segment to learn its size
		d.db = filesystem.NewStorageWithOptions(osfs.New(p), cache.NewObjectLRUDefault(), filesystem.Options{LargeObjectThreshold: 1})
		if info, err := os.Stat(filepath.Join(p, "objects", "pack")); err == nil {
			d.packs = info.ModTime()
		}
	}
	return d.db, p, nil
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
	s, p, err := d.storage()
	if err != nil {
		return nil, "", err
	}
	o, err := s.EncodedObject(plumbing.AnyObject, plumbing.Hash(id.Sum()))
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		o, err = d.again(s, p, id)
	}
	if errors.Is(err, plumbing.ErrObjectNotFound) {
		return nil, "", MissingObject{ID: id}
	}
	if err != nil {
		return nil, "", err
	}
	k, ok := kinds[o.Type()]
	if !ok {
		return nil, "", NotLit{ID: id, Why: fmt.Errorf("a %s, which lit never writes: %w", o.Type(), ErrWrongKind)}
	}
	return o, k, nil
}

// again looks id up once more if a pack has arrived since go-git last read
// the list of packs, which it reads once, so an object a fetch or a repack
// brought is found, not answered missing.
func (d *onDisk) again(s reader, p string, id ObjectID) (plumbing.EncodedObject, error) {
	info, err := os.Stat(filepath.Join(p, "objects", "pack"))
	if d.db == nil || errors.Is(err, fs.ErrNotExist) || err == nil && info.ModTime().Equal(d.packs) {
		return nil, plumbing.ErrObjectNotFound
	}
	if err != nil {
		return nil, err
	}
	d.packs = info.ModTime()
	d.db.Reindex()
	return s.EncodedObject(plumbing.AnyObject, plumbing.Hash(id.Sum()))
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
func (d *onDisk) write(objs []object) (ids []ObjectID, err error) {
	ids = make([]ObjectID, len(objs))
	if len(objs) == 0 {
		return ids, nil
	}
	p, err := d.writable()
	if err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(p), ".objects-")
	if err != nil {
		return nil, err
	}
	// the files' removal failing is the write's failure, never dropped
	// `[LAW:no-silent-failure]`
	defer func() { err = errors.Join(err, os.RemoveAll(tmp)) }()
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
		out, err := d.git(p, []byte(strings.Join(paths[k], "\n")+"\n"), "hash-object", "-w", "-t", string(k), "--no-filters", "--stdin-paths")
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

// writable is the repository's path, created if it is not yet, under a
// git that keeps it durable. Its entry in the directory it is in is
// fsynced on every path, whether this call renamed it into place, lost the
// rename to another, or found it there, since one found may be a racing
// create's, not yet synced: every object written into it is only as
// durable as the name it is under `[LAW:dataflow-not-control-flow]`. Once
// a sync of that directory fails, through this repository or any port of
// the process, every later write of this repository in this process is
// refused with its failure, never handed a repository whose name a
// passing sync might hide was lost `[LAW:no-silent-failure]`; a later run
// knows nothing of it, so the run that saw it is the run that says it.
func (d *onDisk) writable() (string, error) {
	if err := d.g.durable(); err != nil {
		return "", err
	}
	if err := d.create(); err != nil {
		return "", err
	}
	if err := d.entry.Sync(d.at.Parent()); err != nil {
		return "", fmt.Errorf("syncing the entry of lit's repository: %w", err)
	}
	return d.path()
}

// list is every ref under prefix, each name read by go-git on its own, so
// a ref go-git cannot read is broken alone and hides no other. Reading
// never writes: a broken ref is listed as such. A read during a write is safe, since git
// writes a ref as <ref>.lock and renames it into place and writes each
// object whole before any ref names it, so a reader sees a ref as it was
// or as it is.
func (d *onDisk) list(prefix RefName) (Listing, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	l := Listing{Heads: []RefHead{}, Broken: []BrokenRef{}}
	s, p, err := d.storage()
	if err != nil {
		return Listing{}, err
	}
	names, err := refNames(p)
	if err != nil {
		return Listing{}, fmt.Errorf("reading the refs of %s: %w", p, err)
	}
	for _, n := range names {
		if !under(n, prefix) {
			continue
		}
		h, err := ref(s, p, n)
		var broken BrokenRef
		switch {
		case errors.As(err, &broken):
			l.Broken = append(l.Broken, broken)
		case errors.Is(err, plumbing.ErrReferenceNotFound):
		case err != nil:
			return Listing{}, fmt.Errorf("reading the refs of %s: %w", p, err)
		default:
			l.Heads = append(l.Heads, h)
		}
	}
	return l, nil
}

// refNames is the name of every ref the repository at p holds, sorted: each
// file under refs/ git would read as one, any name with a component that
// begins with a dot or ends in .lock left out as git leaves it out, and
// each name packed-refs holds. Only the names are lit's to find; each ref
// is go-git's to read.
func refNames(p string) ([]RefName, error) {
	seen := map[RefName]bool{}
	err := filepath.WalkDir(filepath.Join(p, "refs"), func(path string, e fs.DirEntry, err error) error {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			return err
		case strings.HasPrefix(e.Name(), ".") || strings.HasSuffix(e.Name(), ".lock"):
			if e.IsDir() {
				return fs.SkipDir
			}
			return nil
		case e.IsDir():
			return nil
		}
		rel, err := filepath.Rel(p, path)
		seen[RefName(filepath.ToSlash(rel))] = true
		return err
	})
	if err != nil {
		return nil, err
	}
	packed, err := os.ReadFile(filepath.Join(p, "packed-refs"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	// a packed ref is "<id> <name>"; a header begins with #, and a peeled
	// id, ^<id>, has no name
	for _, line := range strings.Split(string(packed), "\n") {
		if _, n, ok := strings.Cut(line, " "); ok && !strings.HasPrefix(line, "#") {
			seen[RefName(n)] = true
		}
	}
	return slices.Sorted(maps.Keys(seen)), nil
}

// ref is the ref n of the repository at p as go-git reads it, BrokenRef if
// it names no object, or plumbing.ErrReferenceNotFound if it is gone.
func ref(s reader, p string, n RefName) (RefHead, error) {
	r, err := s.Reference(plumbing.ReferenceName(n))
	// go-git reads a loose file it cannot parse as the packed ref, which a
	// name lit found on disk need not have, so not found is gone only if
	// the file is; any other failure to read it is a broken ref, never the
	// listing's
	if err != nil {
		if _, serr := os.Lstat(refPath(p, n)); errors.Is(err, plumbing.ErrReferenceNotFound) && errors.Is(serr, fs.ErrNotExist) {
			return RefHead{}, err
		}
		return RefHead{}, broken(p, n, fmt.Sprintf("go-git cannot read it: %v", err))
	}
	h := r.Hash()
	if r.Type() != plumbing.HashReference || h.IsZero() {
		return RefHead{}, broken(p, n, fmt.Sprintf("it holds %q, no object id", r.Strings()[1]))
	}
	target, err := ObjectIDFrom(SHA1, h[:])
	return RefHead{Name: n, Target: target}, err
}

// broken is the ref n, which names no object, said by what its file holds,
// since go-git reads a torn file as the zero id or the packed ref; packed
// is why, for a name with no loose file.
func broken(p string, n RefName, packed string) BrokenRef {
	b, err := os.ReadFile(refPath(p, n))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return BrokenRef{Name: n, Why: packed}
	case err != nil:
		return BrokenRef{Name: n, Why: err.Error()}
	}
	return BrokenRef{Name: n, Why: fmt.Sprintf("it holds %.80q, no object id", b)}
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
		panic(notAWas(from))
	}
	if err := d.g.durable(); err != nil {
		return err
	}
	p, err := d.path()
	if err != nil {
		return err
	}
	_, err = d.git(p, nil, "update-ref", string(name), to.String(), old)
	if err == nil {
		return nil
	}
	// git refuses a stale old value as it refuses everything else, so the
	// ref's state as read after the refusal, not git's words, says whether
	// it moved: a ref still what from says was git's own failure, Refused
	now, rerr := d.was(name)
	if rerr != nil {
		return errors.Join(err, rerr)
	}
	if now != from {
		return Moved{Name: name, From: from, Now: now}
	}
	return err
}

// was is what the ref name names now, as go-git reads it.
func (d *onDisk) was(name RefName) (Was, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s, p, err := d.storage()
	if err != nil {
		return nil, err
	}
	h, err := ref(s, p, name)
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return Absent{}, nil
	}
	if err != nil {
		return nil, err
	}
	return Named{Target: h.Target}, nil
}

// refPath is the file of the loose ref name in the repository at p.
func refPath(p string, name RefName) string {
	return filepath.Join(p, filepath.FromSlash(string(name)))
}
