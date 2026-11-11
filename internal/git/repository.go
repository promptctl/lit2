package git

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing/cache"
	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/filesystem"
	"github.com/go-git/go-git/v5/storage/memory"
)

// Repository is lit's own object database on disk: a bare git repository at
// its directory, holding lit's objects and refs/lit/**, never the code
// repository's (event-store charter #10). It is created by its first write,
// in a temporary directory renamed into place, so a read creates nothing
// and a reader never sees half a repository.
type Repository struct {
	objectDB
	dir string
}

// RepositoryAt is lit's repository at dir, which need not exist yet.
func RepositoryAt(dir string) Repository {
	r := Repository{dir: dir}
	r.objectDB = objectDB{reach: r.reach}
	return r
}

// reach opens the repository's object storer. A repository not yet created
// holds no objects, which an empty storer says exactly.
func (r Repository) reach(create bool) (storer.EncodedObjectStorer, func(), error) {
	exists, err := r.exists()
	switch {
	case err != nil:
		return nil, nil, err
	case !exists && !create:
		return memory.NewStorage(), func() {}, nil
	case !exists:
		if err := r.create(); err != nil {
			return nil, nil, err
		}
	}
	return filesystem.NewStorage(osfs.New(r.dir), cache.NewObjectLRUDefault()), func() {}, nil
}

func (r Repository) exists() (bool, error) {
	_, err := os.Stat(r.dir)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

// ensure creates the repository unless it exists.
func (r Repository) ensure() error {
	if exists, err := r.exists(); exists || err != nil {
		return err
	}
	return r.create()
}

// create builds a bare repository in a sibling temporary directory and
// renames it into place. Two racing creations each build one; the rename
// that loses finds the directory there, which is the repository it wanted.
func (r Repository) create() error {
	parent := filepath.Dir(r.dir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, ".repo-")
	if err != nil {
		return err
	}
	if err := bare(tmp); err != nil {
		return errors.Join(err, os.RemoveAll(tmp))
	}
	err = os.Rename(tmp, r.dir)
	if errors.Is(err, fs.ErrExist) {
		return os.RemoveAll(tmp)
	}
	if err != nil {
		return errors.Join(err, os.RemoveAll(tmp))
	}
	return nil
}

// bare lays out a bare repository at dir as git init --bare does, enough
// for go-git and for git fsck: HEAD, config, objects and refs.
func bare(dir string) error {
	for _, d := range []string{"objects/info", "objects/pack", "refs/heads", "refs/tags"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.FromSlash(d)), 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "config"), []byte("[core]\n\trepositoryformatversion = 0\n\tbare = true\n"), 0o644)
}

// List reads the refs under prefix from packed-refs and from the loose ref
// files, a loose ref winning over its packed copy, as git reads them.
func (r Repository) List(prefix RefName) ([]RefHead, []BadRef, error) {
	if !strings.HasPrefix(string(prefix), "refs/") {
		return nil, nil, fmt.Errorf("%q is not a prefix under refs/", prefix)
	}
	found := map[RefName]refFile{}
	if err := r.packed(prefix, found); err != nil {
		return nil, nil, err
	}
	if err := r.loose(prefix, found); err != nil {
		return nil, nil, err
	}
	heads, bad := []RefHead{}, []BadRef{}
	for _, name := range slices.Sorted(maps.Keys(found)) {
		f := found[name]
		id, err := ParseObjectID(SHA1, f.text)
		if err != nil {
			bad = append(bad, BadRef{Name: name, Why: fmt.Errorf("%s holds no object id: %w", f.where, err)})
			continue
		}
		heads = append(heads, RefHead{Name: name, Target: id})
	}
	return heads, bad, nil
}

// refFile is one ref's text and the file it was read from.
type refFile struct{ text, where string }

// packed reads packed-refs: "<id> <name>" lines, with # header lines and
// ^<id> lines peeling the tag above them. No packed-refs is no packed ref.
func (r Repository) packed(prefix RefName, found map[RefName]refFile) error {
	where := filepath.Join(r.dir, "packed-refs")
	b, err := os.ReadFile(where)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	lines := bufio.NewScanner(bytes.NewReader(b))
	for lines.Scan() {
		line := lines.Text()
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
			continue // the header, or a peeled tag: neither is a ref
		}
		id, name, ok := strings.Cut(line, " ")
		if !ok {
			return fmt.Errorf("%s holds %q, which is no ref", where, line)
		}
		if strings.HasPrefix(name, string(prefix)) {
			found[RefName(name)] = refFile{text: id, where: where}
		}
	}
	return lines.Err()
}

// loose reads the loose ref files under prefix. A name starting with . is
// no ref, as git's refnames allow none, so the temporary file of a replace
// in flight is never read as one.
func (r Repository) loose(prefix RefName, found map[RefName]refFile) error {
	dir := string(prefix[:strings.LastIndexByte(string(prefix), '/')])
	root := filepath.Join(r.dir, filepath.FromSlash(dir))
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil && p == root && errors.Is(err, fs.ErrNotExist) {
			return fs.SkipAll // no directory under prefix: no loose ref
		}
		if err != nil {
			return err
		}
		if strings.HasPrefix(d.Name(), ".") || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(r.dir, p)
		if err != nil {
			return err
		}
		name := RefName(filepath.ToSlash(rel))
		if !strings.HasPrefix(string(name), string(prefix)) {
			return nil
		}
		if !d.Type().IsRegular() {
			found[name] = refFile{text: "", where: p + " (not a regular file)"}
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		found[name] = refFile{text: strings.TrimSuffix(string(b), "\n"), where: p}
		return nil
	})
}

// Replace points name at to by writing a temporary file beside the loose
// ref and renaming it over the ref, so a reader sees the old ref or the new
// one whole, never an empty file, and no lock is taken (the store's
// decision 6). It creates the repository first, as every write does.
func (r Repository) Replace(name RefName, to ObjectID) error {
	if !strings.HasPrefix(string(name), "refs/") || path.Clean(string(name)) != string(name) {
		return fmt.Errorf("%q is not a refname under refs/", name)
	}
	if _, err := to.hash(); err != nil {
		return err
	}
	if err := r.ensure(); err != nil {
		return err
	}
	file := filepath.Join(r.dir, filepath.FromSlash(string(name)))
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".ref-")
	if err != nil {
		return err
	}
	_, werr := tmp.WriteString(to.String() + "\n")
	if err := errors.Join(werr, tmp.Close()); err != nil {
		return errors.Join(err, os.Remove(tmp.Name()))
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		return errors.Join(err, os.Remove(tmp.Name()))
	}
	return nil
}
