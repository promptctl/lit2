// Package cli is the command line both ways: argv in, text and exit status
// out.
//
// Its layer in architecture.md §6: argv → typed command; rendering; exit
// codes; --json (§5 step 7, path-addressed-cli.md). One pair of tables, the
// verbs and the admin commands, is both the parser and --help, so help
// cannot disagree with the parser `[LAW:one-source-of-truth]`. A flag is
// --name anywhere on the line, -- ends the flags, and - is a value (D4).
package cli

import (
	"fmt"
	"strings"

	"github.com/promptctl/lit2/internal/reason"
)

// Table is one command table, the verbs or the admin commands, in the order
// --help prints it. Only this package makes one, so its rows are the ones
// the parser knows.
type Table struct{ rows []Row }

// Row is one command: its name, the operands it takes, and its line in
// --help.
type Row struct {
	Name     string
	Operands Operands
	Summary  string
}

// Operands is the shape of a row's operands; Make's parameter is the shape,
// so a row cannot make its command from the wrong one.
//
//sumtype:decl
type Operands interface{ operands() }

// TakesPaths is one or more paths: get, rm.
type TakesPaths struct{ Make func([]PathText) Command }

// TakesPairs is one or more path-value pairs: set, add.
type TakesPairs struct{ Make func([]Pair) Command }

// TakesNothing is no operands: the admin commands.
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

// Help is --help anywhere on the line, or a bare lit2.
type Help struct{}

// Get reads the nodes at its paths.
type Get struct{ paths []PathText }

// Rm deletes the nodes at its paths.
type Rm struct{ paths []PathText }

// Set replaces the value at each path.
type Set struct{ pairs []Pair }

// Add appends each value to the list at its path.
type Add struct{ pairs []Pair }

// AdminInit makes this checkout a writer.
type AdminInit struct{}

// AdminSync publishes this checkout's events and fetches every writer's.
type AdminSync struct{}

func (Help) command()      {}
func (Get) command()       {}
func (Rm) command()        {}
func (Set) command()       {}
func (Add) command()       {}
func (AdminInit) command() {}
func (AdminSync) command() {}

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

// adminWord is the word that selects the admin table.
const adminWord = "admin"

// Parse reads argv against the two tables. Its error is a ParseFailure.
func Parse(verbs, admin Table, args []string) (Command, error) {
	words, help, err := scan(args)
	if err != nil {
		return nil, err
	}
	if help || len(words) == 0 {
		return Help{}, nil
	}
	all := known(verbs, admin)
	if words[0] != adminWord {
		return verbs.parse("", all, words)
	}
	if len(words) == 1 {
		return nil, ParseFailure{Code: Usage, Detail: "admin takes a command; the commands are " + all}
	}
	return admin.parse(adminWord+" ", all, words[1:])
}

// scan splits argv into operands and flags. --help is the one flag; -- ends
// the flags; a word that does not start with -- is an operand, - included.
// --help anywhere wins over an unknown flag, which is a usage failure only
// when no --help is on the line, so the whole line is read before judging.
func scan(args []string) (words []string, help bool, err error) {
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
		if unknown == "" {
			unknown = a
		}
	}
	if !help && unknown != "" {
		return nil, false, ParseFailure{Code: Usage, Detail: fmt.Sprintf("unknown flag %s; the one flag is --help", unknown)}
	}
	return words, help, nil
}

// parse finds the row named by words[0] and makes its command from the rest.
func (t Table) parse(prefix, known string, words []string) (Command, error) {
	for _, r := range t.rows {
		if r.Name == words[0] {
			return r.parse(prefix+r.Name, words[1:])
		}
	}
	return nil, ParseFailure{Code: UnknownVerb, Detail: fmt.Sprintf("%q is not a lit2 %scommand; the commands are %s", words[0], prefix, known)}
}

// parse checks the operands against the row's shape and makes its command.
func (r Row) parse(name string, operands []string) (Command, error) {
	switch o := r.Operands.(type) {
	case TakesPaths:
		if len(operands) == 0 {
			return nil, ParseFailure{Code: Usage, Detail: name + " takes one or more paths"}
		}
		paths := make([]PathText, len(operands))
		for i, w := range operands {
			paths[i] = PathText(w)
		}
		return o.Make(paths), nil
	case TakesPairs:
		if len(operands) == 0 || len(operands)%2 != 0 {
			return nil, ParseFailure{Code: Usage, Detail: fmt.Sprintf("%s takes path value pairs; got %d operands", name, len(operands))}
		}
		pairs := make([]Pair, len(operands)/2)
		for i := range pairs {
			pairs[i] = Pair{Path: PathText(operands[2*i]), Value: ValueText(operands[2*i+1])}
		}
		return o.Make(pairs), nil
	case TakesNothing:
		if len(operands) != 0 {
			return nil, ParseFailure{Code: Usage, Detail: fmt.Sprintf("%s takes no operands; got %d", name, len(operands))}
		}
		return o.Make(), nil
	}
	panic(fmt.Sprintf("cli: %T is not Operands", r.Operands))
}

// known names every command the two tables hold, for a failure to list.
func known(verbs, admin Table) string {
	var names []string
	for _, r := range verbs.rows {
		names = append(names, r.Name)
	}
	for _, r := range admin.rows {
		names = append(names, adminWord+" "+r.Name)
	}
	return strings.Join(names, ", ")
}

// ParseFailure is an argv that is not a lit2 command line: class usage.
type ParseFailure struct {
	Code   ParseCode
	Detail string
}

// ParseCode is why an argv is not a lit2 command line.
type ParseCode uint8

// The parse codes. The zero ParseCode is neither.
const (
	// UnknownVerb: the first word names no row, wire name unknown_verb.
	UnknownVerb ParseCode = iota + 1
	// Usage: a row's operands or a flag are wrong, wire name usage.
	Usage
)

func (f ParseFailure) Error() string { return f.Detail }

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
