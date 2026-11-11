package git

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"github.com/go-git/go-git/v5/plumbing/storer"
	"github.com/go-git/go-git/v5/storage/memory"
)

// Memory is lit's repository in process: Objects over go-git's in-memory
// storer, so ids are git's, and refs in a map. It is the twin of
// Repository, held to the same contract suites, and safe for concurrent
// use as the repository on disk is.
type Memory struct {
	objectDB
	mu   sync.Mutex
	refs map[RefName]ObjectID
}

// NewMemory is an empty repository in process.
func NewMemory() *Memory {
	m := &Memory{refs: map[RefName]ObjectID{}}
	objects := memory.NewStorage()
	m.objectDB = objectDB{reach: func(bool) (storer.EncodedObjectStorer, func(), error) {
		m.mu.Lock()
		return objects, m.mu.Unlock, nil
	}}
	return m
}

// List is every ref under prefix, sorted by name. A ref in memory always
// holds an object id, so none is bad.
func (m *Memory) List(prefix RefName) ([]RefHead, []BadRef, error) {
	if !strings.HasPrefix(string(prefix), "refs/") {
		return nil, nil, fmt.Errorf("%q is not a prefix under refs/", prefix)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	heads := []RefHead{}
	for _, name := range slices.Sorted(maps.Keys(m.refs)) {
		if strings.HasPrefix(string(name), string(prefix)) {
			heads = append(heads, RefHead{Name: name, Target: m.refs[name]})
		}
	}
	return heads, []BadRef{}, nil
}

// Replace points name at to.
func (m *Memory) Replace(name RefName, to ObjectID) error {
	if !strings.HasPrefix(string(name), "refs/") {
		return fmt.Errorf("%q is not a refname under refs/", name)
	}
	if _, err := to.hash(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.refs[name] = to
	return nil
}
