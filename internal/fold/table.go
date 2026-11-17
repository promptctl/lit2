package fold

import (
	"encoding/binary"
	"hash/maphash"
	"math/bits"
	"slices"

	"github.com/promptctl/lit2/internal/events"
)

// table is a persistent hash array mapped trie (a CHAMP, Steindorff and
// Vinju's compressed HAMT) from key to record. A write copies the path from
// the root to its leaf, O(log n), and shares the rest, so a State is a
// value and Step returns a new one without touching the old
// `[LAW:types-are-the-program]`. Every collection the fold keeps and grows
// by one per event is one of these, keyed by what identifies its records:
// items by id; the carried, unapplied and malformed records by event, or by
// where and what the malformed record was; and each element, field, set,
// member, create, write, add and id a fact keeps by name or by event. So no
// kept state is quadratic in what it carries. A table holds no order; a
// reader that needs one sorts what it reads.
type table[K comparable, R record[K], H hasher[K]] struct{ root *tnode[K, R] }

// record is a table's record: its key, which no other record of the table
// shares.
type record[K comparable] interface{ key() K }

// hasher hashes a table's keys; it is a type with no fields, so a table
// carries no function value and its zero is the empty table.
type hasher[K comparable] interface{ hash(K) uint64 }

// tnode is one level of the trie: five bits of the key's hash pick a slot,
// which holds either leaf data or a subtree, never both.
type tnode[K comparable, R record[K]] struct {
	dataMap, nodeMap uint32
	data             []tleaf[K, R]  // the slots holding leaves, in bit order
	nodes            []*tnode[K, R] // the slots holding subtrees, in bit order
}

// tleaf is the records whose keys hash alike: one, but for a 64-bit
// collision.
type tleaf[K comparable, R record[K]] struct {
	hash    uint64
	records []R
}

const (
	fanBits = 5
	fanMask = 1<<fanBits - 1
)

// The tables' hashers.
type (
	// byItem is the item id under seed.
	byItem struct{}
	// byElement is the element id under seed.
	byElement struct{}
	// byName is a name's text under seed.
	byName struct{}
	// byEvent is the event id's first eight bytes: the id is a hash.
	byEvent struct{}
	// byRecord is the malformed record's digest's first eight bytes.
	byRecord struct{}
)

// seed keys the hashes of the keys a writer chooses, ids and names, which
// are never digests of anything: an unkeyed hash would let one writer pile
// its keys into one leaf and make every lookup a scan. It is drawn once per
// process and never written again; no output reads a table's order.
var seed = maphash.MakeSeed()

func (byItem) hash(id events.ItemID) uint64 { return maphash.Comparable(seed, id) }

func (byElement) hash(id events.ElementID) uint64 { return maphash.Comparable(seed, id) }

func (byName) hash(name string) uint64 { return maphash.String(seed, name) }

func (byEvent) hash(id events.EventID) uint64 { return binary.LittleEndian.Uint64(id[:8]) }

func (byRecord) hash(k recordAt) uint64 { return binary.LittleEndian.Uint64(k.digest[:8]) }

func slot(h uint64, shift uint) uint32 { return 1 << ((h >> shift) & fanMask) }

func index(m, bit uint32) int { return bits.OnesCount32(m & (bit - 1)) }

// get is the record of k, if the table has one.
func (t table[K, R, H]) get(k K) (R, bool) {
	var hs H
	h := hs.hash(k)
	n := t.root
	for shift := uint(0); n != nil; shift += fanBits {
		bit := slot(h, shift)
		switch {
		case n.dataMap&bit != 0:
			for _, r := range n.data[index(n.dataMap, bit)].records {
				if r.key() == k {
					return r, true
				}
			}
			n = nil
		case n.nodeMap&bit != 0:
			n = n.nodes[index(n.nodeMap, bit)]
		default:
			n = nil
		}
	}
	var none R
	return none, false
}

// set is the table with r as its key's record.
func (t table[K, R, H]) set(r R) table[K, R, H] {
	var hs H
	h := hs.hash(r.key())
	if t.root == nil {
		return table[K, R, H]{root: &tnode[K, R]{dataMap: slot(h, 0), data: []tleaf[K, R]{{hash: h, records: []R{r}}}}}
	}
	return table[K, R, H]{root: t.root.set(h, 0, r)}
}

func (n *tnode[K, R]) set(h uint64, shift uint, r R) *tnode[K, R] {
	bit := slot(h, shift)
	next := *n
	switch {
	case n.dataMap&bit != 0:
		i := index(n.dataMap, bit)
		l := n.data[i]
		if l.hash == h {
			next.data = append([]tleaf[K, R](nil), n.data...)
			next.data[i] = tleaf[K, R]{hash: h, records: replaced(l.records, r)}
			return &next
		}
		// two hashes share this slot: the leaf moves down a level with r
		sub := merged(l, tleaf[K, R]{hash: h, records: []R{r}}, shift+fanBits)
		next.dataMap &^= bit
		next.data = append(append([]tleaf[K, R](nil), n.data[:i]...), n.data[i+1:]...)
		next.nodeMap |= bit
		j := index(next.nodeMap, bit)
		next.nodes = append(append(append([]*tnode[K, R](nil), n.nodes[:j]...), sub), n.nodes[j:]...)
	case n.nodeMap&bit != 0:
		j := index(n.nodeMap, bit)
		next.nodes = append([]*tnode[K, R](nil), n.nodes...)
		next.nodes[j] = n.nodes[j].set(h, shift+fanBits, r)
	default:
		next.dataMap |= bit
		i := index(next.dataMap, bit)
		next.data = append(append(append([]tleaf[K, R](nil), n.data[:i]...), tleaf[K, R]{hash: h, records: []R{r}}), n.data[i:]...)
	}
	return &next
}

// merged is the subtree holding two leaves of different hashes.
func merged[K comparable, R record[K]](a, b tleaf[K, R], shift uint) *tnode[K, R] {
	ab, bb := slot(a.hash, shift), slot(b.hash, shift)
	if ab == bb {
		return &tnode[K, R]{nodeMap: ab, nodes: []*tnode[K, R]{merged(a, b, shift+fanBits)}}
	}
	if index(ab|bb, ab) > 0 {
		a, b = b, a
	}
	return &tnode[K, R]{dataMap: ab | bb, data: []tleaf[K, R]{a, b}}
}

// replaced is rs with r in place of the record of its key, or added.
func replaced[K comparable, R record[K]](rs []R, r R) []R {
	out := append([]R(nil), rs...)
	for i, o := range out {
		if o.key() == r.key() {
			out[i] = r
			return out
		}
	}
	return append(out, r)
}

// each calls f with every record, in no order.
func (t table[K, R, H]) each(f func(R)) {
	var walk func(*tnode[K, R])
	walk = func(n *tnode[K, R]) {
		for _, l := range n.data {
			for _, r := range l.records {
				f(r)
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

// sorted is every record, ordered by compare.
func (t table[K, R, H]) sorted(compare func(a, b R) int) []R {
	var rs []R
	t.each(func(r R) { rs = append(rs, r) })
	slices.SortFunc(rs, compare)
	return rs
}

// least is the record that sorts first under compare, if the table has one.
func (t table[K, R, H]) least(compare func(a, b R) int) (R, bool) {
	var best R
	found := false
	t.each(func(r R) {
		if !found || compare(r, best) < 0 {
			best, found = r, true
		}
	})
	return best, found
}

// empty is whether the table has no record. A table never removes one, so
// its root is nil exactly when nothing was ever set.
func (t table[K, R, H]) empty() bool { return t.root == nil }
