package fold

import (
	"math/bits"

	"github.com/promptctl/lit2/internal/events"
)

// table is the item table: a persistent hash array mapped trie (a CHAMP,
// Steindorff and Vinju's compressed HAMT) from id to item record. A write
// copies the path from the root to its leaf, O(log n), and shares the rest,
// so a State is a value and Step returns a new one without touching the
// old `[LAW:types-are-the-program]`.
type table struct{ root *tnode }

// tnode is one level of the trie: five bits of the id's hash pick a slot,
// which holds either leaf data or a subtree, never both.
type tnode struct {
	dataMap, nodeMap uint32
	data             []tleaf  // the slots holding leaves, in bit order
	nodes            []*tnode // the slots holding subtrees, in bit order
}

// tleaf is the items whose ids hash alike: one, but for a 64-bit collision.
type tleaf struct {
	hash  uint64
	items []*item
}

const (
	fanBits = 5
	fanMask = 1<<fanBits - 1
)

// hashOf is FNV-1a over the id's text.
func hashOf(id events.ItemID) uint64 {
	h := uint64(14695981039346656037)
	for _, c := range []byte(id.String()) {
		h = (h ^ uint64(c)) * 1099511628211
	}
	return h
}

func slot(h uint64, shift uint) uint32 { return 1 << ((h >> shift) & fanMask) }

func index(m, bit uint32) int { return bits.OnesCount32(m & (bit - 1)) }

// get is the record of id, or nil when the table has none.
func (t table) get(id events.ItemID) *item {
	h := hashOf(id)
	n := t.root
	for shift := uint(0); n != nil; shift += fanBits {
		bit := slot(h, shift)
		switch {
		case n.dataMap&bit != 0:
			for _, it := range n.data[index(n.dataMap, bit)].items {
				if it.id == id {
					return it
				}
			}
			return nil
		case n.nodeMap&bit != 0:
			n = n.nodes[index(n.nodeMap, bit)]
		default:
			return nil
		}
	}
	return nil
}

// set is the table with r as its id's record.
func (t table) set(r *item) table {
	h := hashOf(r.id)
	if t.root == nil {
		return table{root: &tnode{dataMap: slot(h, 0), data: []tleaf{{hash: h, items: []*item{r}}}}}
	}
	return table{root: t.root.set(h, 0, r)}
}

func (n *tnode) set(h uint64, shift uint, r *item) *tnode {
	bit := slot(h, shift)
	next := *n
	switch {
	case n.dataMap&bit != 0:
		i := index(n.dataMap, bit)
		l := n.data[i]
		if l.hash == h {
			next.data = append([]tleaf(nil), n.data...)
			next.data[i] = tleaf{hash: h, items: replaced(l.items, r)}
			return &next
		}
		// two hashes share this slot: the leaf moves down a level with r
		sub := merged(l, tleaf{hash: h, items: []*item{r}}, shift+fanBits)
		next.dataMap &^= bit
		next.data = append(append([]tleaf(nil), n.data[:i]...), n.data[i+1:]...)
		next.nodeMap |= bit
		j := index(next.nodeMap, bit)
		next.nodes = append(append(append([]*tnode(nil), n.nodes[:j]...), sub), n.nodes[j:]...)
	case n.nodeMap&bit != 0:
		j := index(n.nodeMap, bit)
		next.nodes = append([]*tnode(nil), n.nodes...)
		next.nodes[j] = n.nodes[j].set(h, shift+fanBits, r)
	default:
		next.dataMap |= bit
		i := index(next.dataMap, bit)
		next.data = append(append(append([]tleaf(nil), n.data[:i]...), tleaf{hash: h, items: []*item{r}}), n.data[i:]...)
	}
	return &next
}

// merged is the subtree holding two leaves of different hashes.
func merged(a, b tleaf, shift uint) *tnode {
	ab, bb := slot(a.hash, shift), slot(b.hash, shift)
	if ab == bb {
		return &tnode{nodeMap: ab, nodes: []*tnode{merged(a, b, shift+fanBits)}}
	}
	if index(ab|bb, ab) > 0 {
		a, b = b, a
	}
	return &tnode{dataMap: ab | bb, data: []tleaf{a, b}}
}

// replaced is items with r in place of the record of its id, or added.
func replaced(items []*item, r *item) []*item {
	out := append([]*item(nil), items...)
	for i, it := range out {
		if it.id == r.id {
			out[i] = r
			return out
		}
	}
	return append(out, r)
}

// each calls f with every record, in no order.
func (t table) each(f func(*item)) {
	var walk func(*tnode)
	walk = func(n *tnode) {
		for _, l := range n.data {
			for _, it := range l.items {
				f(it)
			}
		}
		for _, sub := range n.nodes {
			walk(sub)
		}
	}
	if t.root != nil {
		walk(t.root)
	}
}
