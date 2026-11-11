// Package reason is the exit classes, and the one question every layer's
// failure answers: which class.
//
// It is the exit-class contract and nothing else. Each layer's failure type
// names its own closed set of wire reasons and answers its class, so no
// central list grows with every layer `[LAW:locality-or-seam]`; package cli
// maps a class to its exit status.
package reason

import "fmt"

// Class is what the caller of lit2 does next: the class of a failed
// invocation, from which its exit status follows.
type Class uint8

// The four classes. The zero Class is none of them, so a failure that never
// chose a class cannot pass for one.
const (
	// ClassRefused: the workspace said no and nothing was written.
	ClassRefused Class = iota + 1
	// ClassUsage: the argv is not a lit2 command line.
	ClassUsage
	// ClassEnvironment: no checkout, not initialized, an unparseable
	// workspace file, an unreadable store, a stdout that refuses the answer.
	ClassEnvironment
	// ClassInternal: lit2 failed and recorded why.
	ClassInternal
)

// String is the class's name on the command event's outcome field.
func (c Class) String() string {
	switch c {
	case ClassRefused:
		return "refused"
	case ClassUsage:
		return "usage"
	case ClassEnvironment:
		return "environment"
	case ClassInternal:
		return "internal"
	}
	panic(fmt.Sprintf("reason: %d is not a Class", uint8(c)))
}

// Failure is every layer's failure: an error that answers its class and its
// wire reason, which the error line and the command event both print.
type Failure interface {
	error
	Class() Class
	Reason() string
}
