package cli

import (
	"fmt"

	"github.com/promptctl/lit2/internal/commandname"
	"github.com/promptctl/lit2/internal/verbs"
)

// Lit2 is lit2's command line, in the order --help prints it: the four
// verbs on paths, then admin, what keeps a checkout's store, and config,
// what applies the workspace's versions, both outside the everyday
// surface. A bare lit2 is get /. A row this file gets wrong panics here,
// inside the run's recover, so the run is recorded as lit2's own failure,
// and the test that builds it fails before such a binary ships
// `[LAW:no-silent-failure]`.
func Lit2() Commands {
	c, err := NewCommands([]Row{
		{Name: commandname.Get(), Operands: ReadsPaths{Make: func(p []PathText) Command { return Get{paths: p} }}, Summary: "read the nodes at these paths"},
		{Name: commandname.Set(), Operands: WritesPairs{Make: func(p []Pair, f Flags) Command { return Set{pairs: p, flags: f} }}, Summary: "replace the value at each path"},
		{Name: commandname.Add(), Operands: WritesPairs{Make: func(p []Pair, f Flags) Command { return Add{pairs: p, flags: f} }}, Summary: "append each value to the list at its path"},
		{Name: commandname.Rm(), Operands: WritesPaths{Make: func(p []PathText, f Flags) Command { return Rm{paths: p, flags: f} }}, Summary: "delete the nodes at these paths"},
		{Name: commandname.Init(), Operands: TakesNothing{Make: func() Command { return Init{} }}, Summary: "make this checkout a writer: its id, its principal, the workspace's first version"},
		{Name: commandname.Sync(), Operands: TakesNothing{Make: func() Command { return Sync{} }}, Summary: "publish this checkout's events and fetch and verify every writer's"},
		{Name: commandname.Commit(), Operands: TakesNothing{Make: func() Command { return Commit{} }}, Summary: "apply the committed " + verbs.WorkspacePath() + " as the next version"},
		{Name: commandname.Status(), Operands: TakesNothing{Make: func() Command { return Status{} }}, Summary: "the version in force, the file against it, and any contending"},
	}, Bare{Name: commandname.Get(), Operands: []string{"/"}, Shows: "the tree, the lanes in rank order, what is next, and the version in force"})
	if err != nil {
		panic(fmt.Sprintf("cli: lit2's command line: %v", err))
	}
	return c
}
