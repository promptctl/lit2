package files

import (
	"maps"
	"slices"
	"strings"
	"sync"
)

// Memory is Files in process, the twin of Disk, held to the same contract
// suite and safe for concurrent use.
type Memory struct {
	mu    sync.Mutex
	files map[Name][]byte
}

// NewMemory is an empty directory in process.
func NewMemory() *Memory { return &Memory{files: map[Name][]byte{}} }

// with runs do under the lock once n is a name: the one place Memory
// refuses the zero Name `[LAW:single-enforcer]`.
func (m *Memory) with(n Name, do func() error) error {
	if err := n.valid(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return do()
}

// file runs do on n's bytes under the lock; a missing file is NotFound.
func (m *Memory) file(n Name, do func(f []byte) error) error {
	return m.with(n, func() error {
		f, ok := m.files[n]
		if !ok {
			return NotFound{Name: n}
		}
		return do(f)
	})
}

func (m *Memory) Append(n Name, b []byte) (end uint64, err error) {
	err = m.file(n, func(f []byte) error {
		m.files[n] = append(f, b...)
		end = uint64(len(m.files[n]))
		return nil
	})
	return end, err
}

func (m *Memory) CreateOnce(n Name, b []byte) (made Made, err error) {
	err = m.with(n, func() error {
		if _, ok := m.files[n]; ok {
			made = Existing
			return nil
		}
		m.files[n], made = slices.Clone(b), Created
		return nil
	})
	return made, err
}

func (m *Memory) ReadAt(n Name, off, size uint64) (b []byte, err error) {
	err = m.file(n, func(f []byte) error {
		// by subtraction, so no size overflows the end
		from := min(off, uint64(len(f)))
		b = slices.Clone(f[from : from+min(size, uint64(len(f))-from)])
		return nil
	})
	return b, err
}

func (m *Memory) ReadAll(n Name) (b []byte, err error) {
	err = m.file(n, func(f []byte) error {
		b = slices.Clone(f)
		return nil
	})
	return b, err
}

func (m *Memory) Size(n Name) (size uint64, err error) {
	err = m.file(n, func(f []byte) error {
		size = uint64(len(f))
		return nil
	})
	return size, err
}

// List is every file; a directory in process holds nothing else, so none
// is refused.
func (m *Memory) List() (Listing, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ns := slices.AppendSeq([]Name{}, maps.Keys(m.files))
	slices.SortFunc(ns, func(a, b Name) int { return strings.Compare(a.s, b.s) })
	return Listing{Names: ns, Refused: []Refused{}}, nil
}
