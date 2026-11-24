package files

// Gated is d with gate run on each entry's path between the step's look at
// it and its open, so a test swaps the entry in that gap.
func Gated(d Dir, gate func(path string)) Dir {
	d.gate = gate
	return d
}
