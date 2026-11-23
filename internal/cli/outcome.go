package cli

import (
	"fmt"

	"github.com/promptctl/lit2/internal/reason"
)

// Outcome is how an invocation ended. One Failure carries the status, the
// reason and the message, so the three cannot disagree with what was
// recorded.
//
//sumtype:decl
type Outcome interface{ outcome() }

// Done is an invocation that did what was asked, a dry run or an empty
// answer included.
type Done struct{}

// Failed is an invocation that ended in a failure of its class.
type Failed struct{ Failure reason.Failure }

func (Done) outcome()   {}
func (Failed) outcome() {}

// ExitCode is the one map from an outcome to the process's exit status;
// the process exits with it and the command event records it.
func ExitCode(o Outcome) int {
	switch o := o.(type) {
	case Done:
		return 0
	case Failed:
		return status(o.Failure.Class())
	}
	panic(fmt.Sprintf("cli: %T is not an Outcome", o))
}

// status is each class's exit status, chosen to miss Go's panic status 2 and
// the shell's 126, 127 and 128+n, so any other status means lit2 died
// unrecorded.
func status(c reason.Class) int {
	switch c {
	case reason.ClassRefused:
		return 1
	case reason.ClassUsage:
		return 64 // EX_USAGE
	case reason.ClassInternal:
		return 70 // EX_SOFTWARE
	case reason.ClassEnvironment:
		return 78 // EX_CONFIG
	}
	panic(fmt.Sprintf("cli: %d is not a Class", uint8(c)))
}

// Report is the text an outcome puts on stderr: nothing when Done, the error
// line when Failed.
func Report(o Outcome) string {
	switch o := o.(type) {
	case Done:
		return ""
	case Failed:
		return fmt.Sprintf("error (reason=%s): %s\n", o.Failure.Reason(), o.Failure.Error())
	}
	panic(fmt.Sprintf("cli: %T is not an Outcome", o))
}

// CommandName is the name the command event gives what was asked.
type CommandName string

// Unparsed names an argv that did not parse.
const Unparsed CommandName = "unparsed"

// Summary is what the command event records of a command: its name, its
// paths as typed, and how many values it carried. A value's text is never
// recorded; Values counts them.
type Summary struct {
	Name   CommandName
	Paths  []PathText
	Values int
}

// Summarize is a command's Summary.
func Summarize(c Command) Summary {
	switch c := c.(type) {
	case Help:
		return Summary{Name: "help"}
	case Get:
		return Summary{Name: "get", Paths: c.paths}
	case Rm:
		return Summary{Name: "rm", Paths: c.paths}
	case Set:
		return summarizePairs("set", c.pairs)
	case Add:
		return summarizePairs("add", c.pairs)
	case AdminInit:
		return Summary{Name: "admin init"}
	case AdminSync:
		return Summary{Name: "admin sync"}
	case ConfigCommit:
		return Summary{Name: "config commit"}
	case ConfigStatus:
		return Summary{Name: "config status"}
	}
	panic(fmt.Sprintf("cli: %T is not a Command", c))
}

func summarizePairs(name CommandName, pairs []Pair) Summary {
	paths := make([]PathText, len(pairs))
	for i, p := range pairs {
		paths[i] = p.Path
	}
	return Summary{Name: name, Paths: paths, Values: len(pairs)}
}
