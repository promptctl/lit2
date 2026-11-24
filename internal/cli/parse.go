// Package cli is the command line both ways: argv in, text and exit status
// out.
//
// Its layer in architecture.md §6: argv → typed command; rendering; exit
// codes; --json (§5 step 7, path-addressed-cli.md). The tables, the verbs and
// each group's commands, are both the parser and --help, so help
// cannot disagree with the parser `[LAW:one-source-of-truth]`. A flag is
// --name anywhere on the line, -- ends the flags, and - is a value (D4).
package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/promptctl/lit2/internal/reason"
)

// Table is one command table, the verbs or a group's commands, in the order
// --help prints it. Only this package makes one, so its rows are the ones
// the parser knows.
type Table struct{ rows []Row }

// Row is one command: its name, the operands it takes, whether it takes
// --dry-run, and its line in --help.
type Row struct {
	Name     string
	Operands Operands
	DryRun   bool
	Summary  string
}

// Flags is the flags a command was given beside --help.
type Flags struct{ DryRun bool }

// Operands is the shape of a row's operands; Make's parameter is the shape,
// so a row cannot make its command from the wrong one.
//
//sumtype:decl
type Operands interface{ operands() }

// TakesPaths is one or more paths: get, rm.
type TakesPaths struct {
	Make func([]PathText, Flags) Command
}

// TakesPairs is one or more path-value pairs: set, add.
type TakesPairs struct{ Make func([]Pair, Flags) Command }

// TakesNothing is no operands: a group's commands.
type TakesNothing struct{ Make func() Command }

func (TakesPaths) operands()   {}
func (TakesPairs) operands()   {}
func (TakesNothing) operands() {}

// Command is a parsed command line. Its fields are unexported and Parse is
// the only constructor, so a pathless Get or an odd operand on Set cannot
// exist.
//
//sumtype:decl
type Command interface{ command() }

// Help is --help anywhere on the line.
type Help struct{}

// Get reads the nodes at its paths.
type Get struct{ paths []PathText }

// Rm deletes the nodes at its paths.
type Rm struct {
	paths []PathText
	flags Flags
}

// Set replaces the value at each path.
type Set struct {
	pairs []Pair
	flags Flags
}

// Add appends each value to the list at its path.
type Add struct {
	pairs []Pair
	flags Flags
}

// Paths is the paths get reads.
func (g Get) Paths() []PathText { return g.paths }

// Paths is the paths rm removes.
func (r Rm) Paths() []PathText { return r.paths }

// Pairs is each path set and its value.
func (s Set) Pairs() []Pair { return s.pairs }

// Pairs is each path added to and its value.
func (a Add) Pairs() []Pair { return a.pairs }

// Flags is the flags rm was given.
func (r Rm) Flags() Flags { return r.flags }

// Flags is the flags set was given.
func (s Set) Flags() Flags { return s.flags }

// Flags is the flags add was given.
func (a Add) Flags() Flags { return a.flags }

// AdminInit makes this checkout a writer.
type AdminInit struct{}

// AdminSync publishes this checkout's events and fetches every writer's.
type AdminSync struct{}

// ConfigCommit applies the committed workspace file as the next version.
type ConfigCommit struct{}

// ConfigStatus reports the version in force against the file.
type ConfigStatus struct{}

func (Help) command()         {}
func (Get) command()          {}
func (Rm) command()           {}
func (Set) command()          {}
func (Add) command()          {}
func (AdminInit) command()    {}
func (AdminSync) command()    {}
func (ConfigCommit) command() {}
func (ConfigStatus) command() {}

// PathText is a path as typed. It is not yet an address: a short id
// resolves against the fold, so address parses it at §5 step 4
// `[LAW:parse-dont-validate]`.
type PathText string

// ValueText is a value as typed; the field it is set on reads it.
type ValueText string

// Pair is one path and the value given for it.
type Pair struct {
	Path  PathText
	Value ValueText
}

// Group is a table selected by its first word, outside the verbs: lit2
// admin init, lit2 config commit.
type Group struct {
	Word  string
	Table Table
}

// Parse reads argv against the verbs and the groups. Its error is a
// ParseFailure.
func Parse(verbs Table, groups []Group, args []string) (Command, error) {
	words, help, flags, err := scan(args)
	if err != nil {
		return nil, err
	}
	if help {
		return Help{}, nil
	}
	if len(words) == 0 {
		// a bare lit2 is get /, parsed as typed, so its flags are judged
		// as a get's are
		words = []string{"get", "/"}
	}
	all := known(verbs, groups)
	for _, g := range groups {
		if words[0] != g.Word {
			continue
		}
		if len(words) == 1 {
			return nil, ParseFailure{Code: Usage, Err: fmt.Errorf("%s %w; the commands are %s", g.Word, errNoCommand, all)}
		}
		return g.Table.parse(g.Word+" ", all, words[1:], flags)
	}
	return verbs.parse("", all, words, flags)
}

// scan splits argv into operands and flags. The flags are --help and
// --dry-run; -- ends the flags; a word that does not start with -- is an
// operand, - included. --help anywhere wins over an unknown flag, which is
// a usage failure only when no --help is on the line, so the whole line is
// read before judging.
func scan(args []string) (words []string, help bool, flags Flags, err error) {
	var unknown string
	for i, a := range args {
		if a == "--" {
			words = append(words, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "--") {
			words = append(words, a)
			continue
		}
		if a == "--help" {
			help = true
			continue
		}
		if a == "--dry-run" {
			flags.DryRun = true
			continue
		}
		if unknown == "" {
			unknown = a
		}
	}
	if !help && unknown != "" {
		return nil, false, Flags{}, ParseFailure{Code: Usage, Err: fmt.Errorf("%w %s; the flags are --help and --dry-run", errUnknownFlag, unknown)}
	}
	return words, help, flags, nil
}

// parse finds the row named by words[0] and makes its command from the rest.
func (t Table) parse(prefix, known string, words []string, flags Flags) (Command, error) {
	for _, r := range t.rows {
		if r.Name == words[0] {
			if flags.DryRun && !r.DryRun {
				return nil, ParseFailure{Code: Usage, Err: fmt.Errorf("%s%s %w", prefix, r.Name, errNoDryRun)}
			}
			return r.parse(prefix+r.Name, words[1:], flags)
		}
	}
	return nil, ParseFailure{Code: UnknownVerb, Err: fmt.Errorf("%q %w %scommand; the commands are %s", words[0], errUnknownVerb, prefix, known)}
}

// parse checks the operands against the row's shape and makes its command.
func (r Row) parse(name string, operands []string, flags Flags) (Command, error) {
	switch o := r.Operands.(type) {
	case TakesPaths:
		if len(operands) == 0 {
			return nil, ParseFailure{Code: Usage, Err: fmt.Errorf("%s %w", name, errNoPaths)}
		}
		paths := make([]PathText, len(operands))
		for i, w := range operands {
			paths[i] = PathText(w)
		}
		return o.Make(paths, flags), nil
	case TakesPairs:
		if len(operands) == 0 || len(operands)%2 != 0 {
			return nil, ParseFailure{Code: Usage, Err: fmt.Errorf("%s %w; got %d operands", name, errNotPairs, len(operands))}
		}
		pairs := make([]Pair, len(operands)/2)
		for i := range pairs {
			pairs[i] = Pair{Path: PathText(operands[2*i]), Value: ValueText(operands[2*i+1])}
		}
		return o.Make(pairs, flags), nil
	case TakesNothing:
		if len(operands) != 0 {
			return nil, ParseFailure{Code: Usage, Err: fmt.Errorf("%s %w; got %d", name, errOperands, len(operands))}
		}
		return o.Make(), nil
	}
	panic(fmt.Sprintf("cli: %T is not Operands", r.Operands))
}

// known names every command the tables hold, for a failure to list.
func known(verbs Table, groups []Group) string {
	var names []string
	for _, r := range verbs.rows {
		names = append(names, r.Name)
	}
	for _, g := range groups {
		for _, r := range g.Table.rows {
			names = append(names, g.Word+" "+r.Name)
		}
	}
	return strings.Join(names, ", ")
}

// ParseFailure is an argv that is not a lit2 command line: class usage.
type ParseFailure struct {
	Code ParseCode
	Err  error
}

// The parse refusals, each wrapped in its ParseFailure's Err where it is
// refused: a word that names no command, --dry-run on a command that writes nothing, a flag lit2 has
// not, a group word with no command after it, and operands that are not
// the shape the row takes.
var (
	errUnknownVerb = errors.New("is not a lit2")
	errNoDryRun    = errors.New("takes no --dry-run: only a write does")
	errUnknownFlag = errors.New("unknown flag")
	errNoCommand   = errors.New("takes a command")
	errNoPaths     = errors.New("takes one or more paths")
	errNotPairs    = errors.New("takes path value pairs")
	errOperands    = errors.New("takes no operands")
)

// ParseCode is why an argv is not a lit2 command line.
type ParseCode uint8

// The parse codes. The zero ParseCode is neither.
const (
	// UnknownVerb: the first word names no row, wire name unknown_verb.
	UnknownVerb ParseCode = iota + 1
	// Usage: a row's operands or a flag are wrong, wire name usage.
	Usage
)

func (f ParseFailure) Error() string { return f.Err.Error() }

func (f ParseFailure) Unwrap() error { return f.Err }

// Class is usage for every parse failure.
func (ParseFailure) Class() reason.Class { return reason.ClassUsage }

// Reason is the code's wire name.
func (f ParseFailure) Reason() string {
	switch f.Code {
	case UnknownVerb:
		return "unknown_verb"
	case Usage:
		return "usage"
	}
	panic(fmt.Sprintf("cli: %d is not a ParseCode", uint8(f.Code)))
}
