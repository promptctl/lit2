package files

import "os"

// Writing is d with before run on a file's path as its write begins,
// after its open, so a test lands another writer's write in that gap.
func Writing(d Dir, before func(path string)) Dir {
	s := d.seams
	write := s.Write
	s.Write = func(path string, f *os.File, b []byte) (int, error) {
		before(path)
		return write(path, f, b)
	}
	return d.Seamed(s)
}

// Gated is d with gate run on each entry's path between the step's look at
// it and its open, and on a create-once's temp between the temp and its
// link, so a test swaps the entry, or races a create, in that gap.
func Gated(d Dir, gate func(path string)) Dir {
	s := d.seams
	s.Gate = gate
	return d.Seamed(s)
}
