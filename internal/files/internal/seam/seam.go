// Package seam is the steps a Dir's calls take that a test may stand in
// for: a pause between a look and an open, an fsync, and a temporary
// file's removal. It is internal to files, so only files and its test
// support, filestest, can name or build Seams, and a test gets a seamed Dir
// from filestest `[LAW:locality-or-seam]`.
package seam

import "os"

// Seams is what a Dir's calls do at each seam: Gate runs, given the
// entry's path, between the step's look at it and its open, and between a
// create-once's temporary file and its link; Sync fsyncs f, the file or
// directory at path; Remove removes the entry name in r.
type Seams struct {
	Gate   func(path string)
	Sync   func(path string, f *os.File) error
	Remove func(r *os.Root, name string) error
}

// OS is the seams of every Dir but a test's: no pause, the file's own
// fsync, and the root's own removal.
func OS() Seams {
	return Seams{
		Gate:   func(string) {},
		Sync:   func(_ string, f *os.File) error { return f.Sync() },
		Remove: (*os.Root).Remove,
	}
}
