// Package cli is the command line both ways: argv in, text and exit status
// out.
//
// Its layer in architecture.md §6: argv → typed command; rendering; exit
// codes; --json (§5 step 7, path-addressed-cli.md). The command line, its
// rows and the bare invocation, is the parser, --help and the name the
// command log records, so none of them can disagree with another
// `[LAW:one-source-of-truth]`. A flag is --name anywhere on the line, --
// ends the flags, and - is a value (D4).
package cli

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/promptctl/lit2/internal/commandname"
	"github.com/promptctl/lit2/internal/reason"
)

// The grammar's own tokens: what marks a word a flag, the two flags, and
// the end of the flags.
const (
	flagPrefix = "--"
	helpFlag   = "--help"
	dryRunFlag = "--dry-run"
	endFlags   = "--"
)

// scanned is a line as scan reads it: its operand words, whether --help
// is on it, and the flags it gives the command.
type scanned struct {
	words []string
	help  bool
	flags Flags
}

// flag is one flag scan takes: its name, what --help says it does, and
// what it sets on the line.
type flag struct {
	name, does string
	set        func(*scanned)
}

// theFlags is every flag lit2 takes, the one list scan reads them by and
// --help and the unknown flag's refusal render `[LAW:one-source-of-truth]`.
func theFlags() []flag {
	return []flag{
		{helpFlag, "anywhere prints this", func(s *scanned) { s.help = true }},
		{dryRunFlag, "shows what a write would do", func(s *scanned) { s.flags.DryRun = true }},
	}
}

// flagNames is theFlags' names, said as a list.
func flagNames() string {
	var names []string
	for _, f := range theFlags() {
		names = append(names, f.name)
	}
	return strings.Join(names, " and ")
}

// isFlag is whether scan reads word as a flag, never an operand: the one
// rule scan and NewCommands both read.
func isFlag(word string) bool { return strings.HasPrefix(word, flagPrefix) }

// Commands is the command line: every row, in the order --help prints
// them, and what a bare lit2 runs. NewCommands is its one constructor, so
// a Commands holds no two rows a word line could parse to both.
type Commands struct {
	rows []Row
	bare Bare
	// bareWords is the bare line as scan read it, the words a bare lit2
	// routes as a typed line's
	bareWords []string
}

// Row is one command: its name, the operands it takes, and its line in
// --help. The name's group is the group --help prints it under, so
// regrouping a row is renaming it; whether it takes --dry-run is its
// operands' shape's.
type Row struct {
	Name     commandname.Name
	Operands Operands
	Summary  string
}

// Bare is what lit2 with no words runs: the row named Name, over
// Operands, which --help says Shows.
type Bare struct {
	Name     commandname.Name
	Operands []string
	Shows    string
}

// NewCommands is the command line of rows, run bare as bare. It refuses
// a line that does not parse one way `[LAW:single-enforcer]`: a row with
// no operand shape or no Make; a name word that is empty, that scan reads
// as a flag, or that holds a space, which the log, joining a name's words
// by spaces, would spell as two; a name a word line could reach two rows
// by, one of them a prefix of the other or both the same; a reserved
// name; and a bare invocation scan does not read word for word as typed,
// or that does not route, as a typed line does, to its row. It keeps
// copies of rows and bare, so what it checked is what it holds.
func NewCommands(rows []Row, bare Bare) (Commands, error) {
	for i, r := range rows {
		if r.Operands == nil || !r.Operands.makes() {
			return Commands{}, fmt.Errorf("%w: %q", errNoMake, r.Name)
		}
		for _, w := range r.Name.Words() {
			if w == "" || isFlag(w) || strings.ContainsFunc(w, unicode.IsSpace) {
				return Commands{}, fmt.Errorf("%w: %q in %q", errNotAWord, w, r.Name)
			}
		}
		if slices.Contains(commandname.Reserved(), r.Name) {
			return Commands{}, fmt.Errorf("%w: %q", errReservedName, r.Name)
		}
		for _, o := range rows[:i] {
			if begins(o.Name.Words(), r.Name.Words()) || begins(r.Name.Words(), o.Name.Words()) {
				return Commands{}, fmt.Errorf("%w: %q and %q", errAmbiguous, o.Name, r.Name)
			}
		}
	}
	typed := append(bare.Name.Words(), bare.Operands...)
	s, err := scan(typed)
	if err != nil || s.help || s.flags != (Flags{}) || !slices.Equal(s.words, typed) {
		return Commands{}, fmt.Errorf("%w: %q", errBareNotAsTyped, typed)
	}
	bare.Operands = slices.Clone(bare.Operands)
	c := Commands{rows: slices.Clone(rows), bare: bare, bareWords: s.words}
	// %v, not %w: the row's refusal is this line's bug, never a usage
	// failure a caller could take for the person's
	if p, err := c.route(s.words, Flags{}); err != nil || p.Asked.Name != bare.Name {
		return Commands{}, fmt.Errorf("%w: %q: %v", errBareUnrouted, typed, err)
	}
	return c, nil
}

// Rows is c's rows, in --help's order, for a caller to range over or to
// build another line from.
func (c Commands) Rows() []Row { return slices.Clone(c.rows) }

// Bare is what a bare lit2 runs on c.
func (c Commands) Bare() Bare {
	b := c.bare
	b.Operands = slices.Clone(b.Operands)
	return b
}

// begins is whether words begin with all of name's: the one test of
// which row a word line reaches, Parse's and NewCommands's.
func begins(name, words []string) bool {
	return len(name) <= len(words) && slices.Equal(name, words[:len(name)])
}

// The table refusals, each the cause of NewCommands's error.
var (
	errNoMake         = errors.New("a row with no operand shape or no Make")
	errNotAWord       = errors.New("a name word that is not one operand word")
	errReservedName   = errors.New("a reserved name")
	errAmbiguous      = errors.New("names a word line reaches both of")
	errBareNotAsTyped = errors.New("a bare invocation scan does not read as typed")
	errBareUnrouted   = errors.New("a bare invocation that does not parse as its row's")
)

// Flags is the flags a command was given beside --help.
type Flags struct{ DryRun bool }

// Operands is what a row's Make is handed: its operands, in their shape,
// and the flags, when the command has a dry run. Make's parameters are the
// shape, so a row cannot make its command from the wrong operands, and a
// row takes --dry-run exactly when its command is handed it, so none takes
// it and ignores it `[LAW:types-are-the-program]`.
//
//sumtype:decl
type Operands interface {
	operands()
	// makes is whether the shape has its Make, which NewCommands requires.
	makes() bool
	// dryRuns is whether Make is handed --dry-run.
	dryRuns() bool
}

// TakesPaths is one or more paths, with no dry run: get.
type TakesPaths struct{ Make func([]PathText) Command }

// TakesPathsDryRun is one or more paths and the flags: rm.
type TakesPathsDryRun struct {
	Make func([]PathText, Flags) Command
}

// TakesPairsDryRun is one or more path-value pairs and the flags: set, add.
type TakesPairsDryRun struct{ Make func([]Pair, Flags) Command }

// TakesNothing is no operands, with no dry run: a group's commands.
type TakesNothing struct{ Make func() Command }

func (TakesPaths) operands()           {}
func (TakesPathsDryRun) operands()     {}
func (TakesPairsDryRun) operands()     {}
func (TakesNothing) operands()         {}
func (o TakesPaths) makes() bool       { return o.Make != nil }
func (o TakesPathsDryRun) makes() bool { return o.Make != nil }
func (o TakesPairsDryRun) makes() bool { return o.Make != nil }
func (o TakesNothing) makes() bool     { return o.Make != nil }
func (TakesPaths) dryRuns() bool       { return false }
func (TakesPathsDryRun) dryRuns() bool { return true }
func (TakesPairsDryRun) dryRuns() bool { return true }
func (TakesNothing) dryRuns() bool     { return false }

// Command is a parsed command line. Its fields are unexported and Parse is
// the only constructor, so a pathless Get or an odd operand on Set cannot
// exist; its accessors hand out copies, so no body changes what was parsed.
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
func (g Get) Paths() []PathText { return slices.Clone(g.paths) }

// Paths is the paths rm removes.
func (r Rm) Paths() []PathText { return slices.Clone(r.paths) }

// Pairs is each path set and its value.
func (s Set) Pairs() []Pair { return slices.Clone(s.pairs) }

// Pairs is each path added to and its value.
func (a Add) Pairs() []Pair { return slices.Clone(a.pairs) }

// Flags is the flags rm was given.
func (r Rm) Flags() Flags { return r.flags }

// Flags is the flags set was given.
func (s Set) Flags() Flags { return s.flags }

// Flags is the flags add was given.
func (a Add) Flags() Flags { return a.flags }

// Init makes this checkout a writer.
type Init struct{}

// Sync publishes this checkout's events and fetches every writer's.
type Sync struct{}

// Commit applies the committed workspace file as the next version.
type Commit struct{}

// Status reports the version in force against the file.
type Status struct{}

func (Help) command()   {}
func (Get) command()    {}
func (Rm) command()     {}
func (Set) command()    {}
func (Add) command()    {}
func (Init) command()   {}
func (Sync) command()   {}
func (Commit) command() {}
func (Status) command() {}

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

// Parsed is a command line that parsed: the command, and what the command
// event records of it, made by the row that parsed it, so the name logged
// is the name typed `[LAW:one-source-of-truth]`.
type Parsed struct {
	Asked   Summary
	Command Command
}

// Parse reads argv against c. Its error is a ParseFailure.
func Parse(c Commands, args []string) (Parsed, error) {
	s, err := scan(args)
	if err != nil {
		return Parsed{}, err
	}
	if s.help {
		return Parsed{Asked: Summary{Name: commandname.Help()}, Command: Help{}}, nil
	}
	words := s.words
	if len(words) == 0 {
		// a bare lit2 is its bare line, read by scan as a typed one is, so
		// the flags typed with it are judged as its row's are
		words = c.bareWords
	}
	return c.route(words, s.flags)
}

// route is the command words parse to on c, given flags: the row the
// words begin with, its operands the rest.
func (c Commands) route(words []string, flags Flags) (Parsed, error) {
	for _, r := range c.rows {
		name := r.Name.Words()
		if !begins(name, words) {
			continue
		}
		if flags.DryRun && !r.Operands.dryRuns() {
			dry := c.named(func(r Row) bool { return r.Operands.dryRuns() })
			return Parsed{}, refuseUsage(fmt.Errorf("%s: %w; the commands with a dry run are %s", r.Name, errNoDryRun, dry))
		}
		return r.parse(words[len(name):], flags)
	}
	if !c.isGroup(words[0]) {
		return Parsed{}, UnknownVerb{word: words[0], known: c.known()}
	}
	if len(words) == 1 {
		return Parsed{}, refuseUsage(fmt.Errorf("%s: %w; the commands are %s", words[0], errNoCommand, c.known()))
	}
	return Parsed{}, UnknownVerb{prefix: words[0] + " ", word: words[1], known: c.known()}
}

// scan splits argv into operands and the flags theFlags names; -- ends
// the flags; a word that is no flag is an operand, - included. --help
// anywhere wins over an unknown flag, which is a usage failure only when
// no --help is on the line, so the whole line is read before judging.
func scan(args []string) (scanned, error) {
	var s scanned
	var unknown string
	for i, a := range args {
		if a == endFlags {
			s.words = append(s.words, args[i+1:]...)
			break
		}
		if !isFlag(a) {
			s.words = append(s.words, a)
			continue
		}
		if f := slices.IndexFunc(theFlags(), func(f flag) bool { return f.name == a }); f >= 0 {
			theFlags()[f].set(&s)
			continue
		}
		if unknown == "" {
			unknown = a
		}
	}
	if !s.help && unknown != "" {
		return scanned{}, refuseUsage(fmt.Errorf("%w %s; the flags are %s", errUnknownFlag, unknown, flagNames()))
	}
	return s, nil
}

// parse checks the operands against the row's shape and makes its
// command, with the summary of it the event records, which shares the
// command's paths: the command hands out only copies of them.
func (r Row) parse(operands []string, flags Flags) (Parsed, error) {
	switch o := r.Operands.(type) {
	case TakesPaths:
		paths, err := r.paths(operands)
		if err != nil {
			return Parsed{}, err
		}
		return Parsed{Asked: Summary{Name: r.Name, Paths: paths}, Command: o.Make(paths)}, nil
	case TakesPathsDryRun:
		paths, err := r.paths(operands)
		if err != nil {
			return Parsed{}, err
		}
		return Parsed{Asked: Summary{Name: r.Name, Paths: paths}, Command: o.Make(paths, flags)}, nil
	case TakesPairsDryRun:
		if len(operands) == 0 || len(operands)%2 != 0 {
			return Parsed{}, refuseUsage(fmt.Errorf("%s: %w; got %d operands", r.Name, errNotPairs, len(operands)))
		}
		pairs, asked := make([]Pair, len(operands)/2), make([]PathText, len(operands)/2)
		for i := range pairs {
			pairs[i] = Pair{Path: PathText(operands[2*i]), Value: ValueText(operands[2*i+1])}
			asked[i] = pairs[i].Path
		}
		return Parsed{Asked: Summary{Name: r.Name, Paths: asked, Values: len(pairs)}, Command: o.Make(pairs, flags)}, nil
	case TakesNothing:
		if len(operands) != 0 {
			return Parsed{}, refuseUsage(fmt.Errorf("%s: %w; it takes none, got %d", r.Name, errOperands, len(operands)))
		}
		return Parsed{Asked: Summary{Name: r.Name}, Command: o.Make()}, nil
	}
	panic(fmt.Sprintf("cli: %T is not Operands", r.Operands))
}

// paths is operands as the one or more paths r takes.
func (r Row) paths(operands []string) ([]PathText, error) {
	if len(operands) == 0 {
		return nil, refuseUsage(fmt.Errorf("%s: %w; it takes one or more", r.Name, errNoPaths))
	}
	paths := make([]PathText, len(operands))
	for i, w := range operands {
		paths[i] = PathText(w)
	}
	return paths, nil
}

// isGroup is whether word is a group's: the first of some row's two
// words.
func (c Commands) isGroup(word string) bool {
	return slices.ContainsFunc(c.rows, func(r Row) bool {
		g, in := r.Name.Group()
		return in && g == word
	})
}

// known names every command c holds, for a failure to list.
func (c Commands) known() string { return c.named(func(Row) bool { return true }) }

// named names the commands of c's rows that are, for a failure to list.
func (c Commands) named(are func(Row) bool) string {
	var names []string
	for _, r := range c.rows {
		if are(r) {
			names = append(names, r.Name.String())
		}
	}
	return strings.Join(names, ", ")
}

// ParseFailure is an argv that is not a lit2 command line, class usage: an
// UnknownVerb or a Usage. Each refusal site builds the one its refusal is,
// so the type it chose is its wire reason `[LAW:types-are-the-program]`.
//
//sumtype:decl
type ParseFailure interface {
	reason.Failure
	parseFailure()
}

// UnknownVerb is a word that names no command: wire reason unknown_verb.
// Its cause is always errUnknownVerb, so it holds only the words it was
// refused with and no cause a site could get wrong.
type UnknownVerb struct{ prefix, word, known string }

// Usage is a row's operands or a flag that are wrong: wire reason usage.
type Usage struct{ cause error }

// refuseUsage is the Usage caused by cause.
func refuseUsage(cause error) Usage { return Usage{cause: cause} }

// The parse refusals, each the cause of its ParseFailure, wrapped where it
// is refused with the words that were refused.
var (
	errUnknownVerb = errors.New("unknown command")
	errNoDryRun    = errors.New("unsupported " + dryRunFlag)
	errUnknownFlag = errors.New("unknown flag")
	errNoCommand   = errors.New("missing command")
	errNoPaths     = errors.New("missing paths")
	errNotPairs    = errors.New("operands not in path value pairs")
	errOperands    = errors.New("unexpected operands")
)

func (u UnknownVerb) Error() string {
	return fmt.Sprintf("%s%q: %v; the commands are %s", u.prefix, u.word, errUnknownVerb, u.known)
}

func (UnknownVerb) Unwrap() error { return errUnknownVerb }

func (UnknownVerb) Class() reason.Class { return reason.ClassUsage }

func (UnknownVerb) Reason() string { return "unknown_verb" }

func (UnknownVerb) parseFailure() {}

func (u Usage) Error() string { return u.cause.Error() }

func (u Usage) Unwrap() error { return u.cause }

func (Usage) Class() reason.Class { return reason.ClassUsage }

func (Usage) Reason() string { return "usage" }

func (Usage) parseFailure() {}
