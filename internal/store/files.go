package store

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// Path names a file under the store's root, slash-separated. Only the
// store's layout builds one.
type Path string

// Files appends to and atomically replaces plain files under the store's
// root. It is the port every local byte the store keeps goes through.
type Files interface {
	// Append writes b at the end of the file in one write on an O_APPEND
	// descriptor, then fsyncs, and returns the file's offset after it; so
	// concurrent appends land whole with no lock. A missing file is
	// NotFound.
	Append(p Path, b []byte) (end uint64, err error)
	// CreateOnce creates the file holding b unless it exists: b is written
	// to a temporary file, fsynced and linked into place, so exactly one
	// creation wins and the file is never seen, nor left by a crash,
	// partly written. On disk this needs hard links, since link(2) is the
	// one call that both refuses an existing name and puts whole bytes
	// under it; a filesystem without them fails the first append, saying so.
	CreateOnce(p Path, b []byte) (Made, error)
	// ReadAt reads up to n bytes at off, fewer at the end of the file.
	ReadAt(p Path, off, n uint64) ([]byte, error)
	Size(p Path) (uint64, error)
	// List is the files directly in dir, sorted; a missing dir holds none.
	List(dir Path) ([]Path, error)
}

// Made is the outcome of a create-once: the file was created, or found
// there already.
type Made uint8

// The two answers of CreateOnce.
const (
	Existing Made = iota + 1
	Created
)

// NotFound is a file that does not exist.
type NotFound struct{ Path Path }

func (n NotFound) Error() string { return fmt.Sprintf("%s does not exist in the store", n.Path) }

// OSFiles is Files on disk, under the directory root, every file it makes
// in one mode.
type OSFiles struct {
	root string
	mode fs.FileMode
}

// FilesAt is the store's files under root, which need not exist yet. Every
// file it makes is 0644, as go-git makes lit's objects, so whoever can read
// a clone's objects can read its logs too.
func FilesAt(root string) OSFiles { return FilesAtMode(root, 0o644) }

// FilesAtMode is files under root, each made in mode: the identity files,
// a secret among them, are 0600 through this same port.
func FilesAtMode(root string, mode fs.FileMode) OSFiles { return OSFiles{root: root, mode: mode} }

func (o OSFiles) file(p Path) string { return filepath.Join(o.root, filepath.FromSlash(string(p))) }

// missing is err as NotFound when it says the file does not exist.
func missing(p Path, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return NotFound{Path: p}
	}
	return err
}

func (o OSFiles) Append(p Path, b []byte) (end uint64, err error) {
	f, err := os.OpenFile(o.file(p), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		return 0, missing(p, err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	if _, err := f.Write(b); err != nil {
		return 0, err
	}
	// a printed echo must survive a power loss, so the append is not done
	// until it is on disk (the store's decision 11)
	if err := f.Sync(); err != nil {
		return 0, err
	}
	at, err := f.Seek(0, io.SeekCurrent)
	return uint64(at), err
}

func (o OSFiles) CreateOnce(p Path, b []byte) (made Made, err error) {
	dir := filepath.Dir(o.file(p))
	f, err := o.temp(dir, b)
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, os.Remove(f.Name())) }()
	// the bytes are on disk before their name is, so a power loss leaves no
	// file or a whole one, never an empty one every append then refuses
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		return 0, err
	}
	err = os.Link(f.Name(), o.file(p))
	if errors.Is(err, fs.ErrExist) {
		return Existing, nil
	}
	if err != nil {
		return 0, err
	}
	// the new name must survive a power loss as the appends to it will
	return Created, syncDir(dir)
}

func syncDir(dir string) (err error) {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, d.Close()) }()
	return d.Sync()
}

// temp writes b to a new temporary file in dir, creating dir, and returns
// it open, in the instance's mode, in a 0755 directory. Its name starts
// with ., which List never reports.
func (o OSFiles) temp(dir string, b []byte) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		return nil, err
	}
	_, werr := f.Write(b)
	if err := errors.Join(werr, f.Chmod(o.mode)); err != nil {
		return nil, errors.Join(err, f.Close(), os.Remove(f.Name()))
	}
	return f, nil
}

func (o OSFiles) ReadAt(p Path, off, n uint64) (b []byte, err error) {
	f, err := os.Open(o.file(p))
	if err != nil {
		return nil, missing(p, err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	return io.ReadAll(io.NewSectionReader(f, int64(off), int64(n)))
}

func (o OSFiles) Size(p Path) (uint64, error) {
	info, err := os.Stat(o.file(p))
	if err != nil {
		return 0, missing(p, err)
	}
	return uint64(info.Size()), nil
}

func (o OSFiles) List(dir Path) ([]Path, error) {
	entries, err := os.ReadDir(o.file(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return []Path{}, nil // nothing is created by a read: no directory yet holds no file
	}
	if err != nil {
		return nil, err
	}
	ps := []Path{}
	for _, e := range entries {
		if e.Type().IsRegular() && !strings.HasPrefix(e.Name(), ".") {
			ps = append(ps, Path(path.Join(string(dir), e.Name())))
		}
	}
	return ps, nil
}

// MemoryFiles is Files in process, the twin of OSFiles, held to the same
// contract suite and safe for concurrent use.
type MemoryFiles struct {
	mu    sync.Mutex
	files map[Path][]byte
}

// NewMemoryFiles is an empty store root in process.
func NewMemoryFiles() *MemoryFiles { return &MemoryFiles{files: map[Path][]byte{}} }

func (m *MemoryFiles) Append(p Path, b []byte) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[p]
	if !ok {
		return 0, NotFound{Path: p}
	}
	m.files[p] = append(f, b...)
	return uint64(len(m.files[p])), nil
}

func (m *MemoryFiles) CreateOnce(p Path, b []byte) (Made, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[p]; ok {
		return Existing, nil
	}
	m.files[p] = slices.Clone(b)
	return Created, nil
}

func (m *MemoryFiles) ReadAt(p Path, off, n uint64) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[p]
	if !ok {
		return nil, NotFound{Path: p}
	}
	from := min(off, uint64(len(f)))
	return slices.Clone(f[from:min(from+n, uint64(len(f)))]), nil
}

func (m *MemoryFiles) Size(p Path) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[p]
	if !ok {
		return 0, NotFound{Path: p}
	}
	return uint64(len(f)), nil
}

func (m *MemoryFiles) List(dir Path) ([]Path, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ps := []Path{}
	for _, p := range slices.Sorted(maps.Keys(m.files)) {
		if path.Dir(string(p)) == string(dir) {
			ps = append(ps, p)
		}
	}
	return ps, nil
}
