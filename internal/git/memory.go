package git

import (
	"fmt"
	"maps"
	"slices"
	"sync"
)

// Memory is lit's repository in process: every object under the id lit's
// encoding computes, which gittest holds to the id git computes, and refs
// in a map under git's rules. It is the twin of Repository, held to the
// same contract suites, and safe for concurrent use as the repository on
// disk is.
type Memory struct{ repo }

// NewMemory is an empty repository in process. It holds the empty tree,
// as every git repository does without storing it.
func NewMemory() Memory {
	empty := tree(nil)
	return Memory{repo{b: &memory{objects: map[ObjectID]stored{idOf(empty): {kind: treeKind}}, refs: map[RefName]ObjectID{}}}}
}

type stored struct {
	kind  kind
	bytes []byte
}

type memory struct {
	mu      sync.Mutex
	objects map[ObjectID]stored
	refs    map[RefName]ObjectID
}

func (m *memory) look(id ObjectID) (kind, uint64, error) {
	k, b, err := m.read(id)
	return k, uint64(len(b)), err
}

func (m *memory) read(id ObjectID) (kind, []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[id]
	if !ok {
		return "", nil, MissingObject{ID: id}
	}
	return o.kind, slices.Clone(o.bytes), nil
}

func (m *memory) write(o object) (ObjectID, error) {
	k, b := o.encode()
	id := hashObject(SHA1, k, b)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[id] = stored{kind: k, bytes: slices.Clone(b)}
	return id, nil
}

func (m *memory) list(prefix RefName) ([]RefHead, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	heads := []RefHead{}
	for _, n := range slices.Sorted(maps.Keys(m.refs)) {
		if under(n, prefix) {
			heads = append(heads, RefHead{Name: n, Target: m.refs[n]})
		}
	}
	return heads, nil
}

// replace refuses as git update-ref does: a ref that no longer is what
// from says, and a name that is a directory of another ref, or has one as
// its directory, as git's refs, which are paths, cannot hold both.
func (m *memory) replace(name RefName, from Was, to ObjectID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	target, ok := m.refs[name]
	now := Was(Absent{})
	if ok {
		now = Named{Target: target}
	}
	if now != from {
		return fmt.Errorf("%s is %v, not %v", name, now, from)
	}
	for n := range m.refs {
		if n != name && (under(n, name) || under(name, n)) {
			return fmt.Errorf("%s cannot be a ref beside %s", name, n)
		}
	}
	m.refs[name] = to
	return nil
}
