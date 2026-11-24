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

func (m *Memory) Append(n Name, b []byte) (uint64, error) {
	if err := n.valid(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[n]
	if !ok {
		return 0, NotFound{Name: n}
	}
	m.files[n] = append(f, b...)
	return uint64(len(m.files[n])), nil
}

func (m *Memory) CreateOnce(n Name, b []byte) (Made, error) {
	if err := n.valid(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[n]; ok {
		return Existing, nil
	}
	m.files[n] = slices.Clone(b)
	return Created, nil
}

func (m *Memory) ReadAt(n Name, off, size uint64) ([]byte, error) {
	if err := n.valid(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[n]
	if !ok {
		return nil, NotFound{Name: n}
	}
	// by subtraction, so no size overflows the end
	from := min(off, uint64(len(f)))
	return slices.Clone(f[from : from+min(size, uint64(len(f))-from)]), nil
}

func (m *Memory) Size(n Name) (uint64, error) {
	if err := n.valid(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.files[n]
	if !ok {
		return 0, NotFound{Name: n}
	}
	return uint64(len(f)), nil
}

func (m *Memory) List() ([]Name, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ns := slices.AppendSeq([]Name{}, maps.Keys(m.files))
	slices.SortFunc(ns, func(a, b Name) int { return strings.Compare(a.s, b.s) })
	return ns, nil
}
