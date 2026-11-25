package files

// Gated is d with gate run on each entry's path between the step's look at
// it and its open, and on a create-once's temp between the temp and its
// link, so a test swaps the entry, or races a create, in that gap.
func Gated(d Dir, gate func(path string)) Dir {
	s := d.seams
	s.Gate = gate
	return d.Seamed(s)
}
