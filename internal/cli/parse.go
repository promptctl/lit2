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

// flag is one flag scan takes, and what --help says it does.
type flag struct{ name, does string }

// theFlags is every flag scan takes, the one list --help and the unknown
// flag's refusal render.
func theFlags() []flag {
	return []flag{{helpFlag, "anywhere prints this"}, {dryRunFlag, "shows what a write would do"}}
}

// flagNames is theFlags' names, said as a list.
func flagNames() string {
	names := make([]string, 0, 2)
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
}

// Row is one command: its name, the operands it takes, whether it takes
// --dry-run, and its line in --help. The name's group is the group --help
// prints it under, so regrouping a row is renaming it.
type Row struct {
	Name     commandname.Name
	Operands Operands
	DryRun   bool
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
// name; and a bare invocation that is no row's or does not parse as its
// row's. It keeps copies of rows and bare, so what it checked is what it
// holds.
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
			if reaches(o.Name, r.Name) || reaches(r.Name, o.Name) {
				return Commands{}, fmt.Errorf("%w: %q and %q", errAmbiguous, o.Name, r.Name)
			}
		}
	}
	i := slices.IndexFunc(rows, func(r Row) bool { return r.Name == bare.Name })
	if i < 0 {
		return Commands{}, fmt.Errorf("%w: %q", errBareNoRow, bare.Name)
	}
	// %v, not %w: the row's refusal is this line's bug, never a usage
	// failure a caller could take for the person's
	if _, err := rows[i].parse(bare.Operands, Flags{}); err != nil {
		return Commands{}, fmt.Errorf("%w: %v", errBareUnparsed, err)
	}
	bare.Operands = slices.Clone(bare.Operands)
	return Commands{rows: slices.Clone(rows), bare: bare}, nil
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

// reaches is whether a word line that reaches a also reaches b: b's words
// begin with all of a's.
func reaches(a, b commandname.Name) bool {
	aw, bw := a.Words(), b.Words()
	return len(aw) <= len(bw) && slices.Equal(aw, bw[:len(aw)])
}

// The table refusals, each the cause of NewCommands's error.
var (
	errNoMake       = errors.New("a row with no operand shape or no Make")
	errNotAWord     = errors.New("a name word that is not one operand word")
	errReservedName = errors.New("a reserved name")
	errAmbiguous    = errors.New("names a word line reaches both of")
	errBareNoRow    = errors.New("a bare invocation of no row")
	errBareUnparsed = errors.New("a bare invocation its row does not parse")
)

// Flags is the flags a command was given beside --help.
type Flags struct{ DryRun bool }

// Operands is the shape of a row's operands; Make's parameter is the shape,
// so a row cannot make its command from the wrong one.
//
//sumtype:decl
type Operands interface {
	operands()
	// makes is whether the shape has its Make, which NewCommands requires.
	makes() bool
}

// TakesPaths is one or more paths: get, rm.
type TakesPaths struct {
	Make func([]PathText, Flags) Command
}

// TakesPairs is one or more path-value pairs: set, add.
type TakesPairs struct{ Make func([]Pair, Flags) Command }

// TakesNothing is no operands: a group's commands.
type TakesNothing struct{ Make func() Command }

func (TakesPaths) operands()       {}
func (TakesPairs) operands()       {}
func (o TakesPaths) makes() bool   { return o.Make != nil }
func (o TakesPairs) makes() bool   { return o.Make != nil }
func (o TakesNothing) makes() bool { return o.Make != nil }
func (TakesNothing) operands()     {}

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

// Parsed is a command line that parsed: the command, and what the command
// event records of it, made by the row that parsed it, so the name logged
// is the name typed `[LAW:one-source-of-truth]`.
type Parsed struct {
	Asked   Summary
	Command Command
}

// Parse reads argv against c. Its error is a ParseFailure.
func Parse(c Commands, args []string) (Parsed, error) {
	words, help, flags, err := scan(args)
	if err != nil {
		return Parsed{}, err
	}
	if help {
		return Parsed{Asked: Summary{Name: commandname.Help()}, Command: Help{}}, nil
	}
	if len(words) == 0 {
		// a bare lit2 is its bare row, parsed as typed, so its flags are
		// judged as that row's are
		words = append(c.bare.Name.Words(), c.bare.Operands...)
	}
	for _, r := range c.rows {
		name := r.Name.Words()
		if len(words) < len(name) || !slices.Equal(words[:len(name)], name) {
			continue
		}
		if flags.DryRun && !r.DryRun {
			return Parsed{}, refuseUsage(fmt.Errorf("%s: %w; only a write takes it", r.Name, errNoDryRun))
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

// scan splits argv into operands and flags. The flags are --help and
// --dry-run; -- ends the flags; a word that does not start with -- is an
// operand, - included. --help anywhere wins over an unknown flag, which is
// a usage failure only when no --help is on the line, so the whole line is
// read before judging.
func scan(args []string) (words []string, help bool, flags Flags, err error) {
	var unknown string
	for i, a := range args {
		if a == endFlags {
			words = append(words, args[i+1:]...)
			break
		}
		if !isFlag(a) {
			words = append(words, a)
			continue
		}
		if a == helpFlag {
			help = true
			continue
		}
		if a == dryRunFlag {
			flags.DryRun = true
			continue
		}
		if unknown == "" {
			unknown = a
		}
	}
	if !help && unknown != "" {
		return nil, false, Flags{}, refuseUsage(fmt.Errorf("%w %s; the flags are %s", errUnknownFlag, unknown, flagNames()))
	}
	return words, help, flags, nil
}

// parse checks the operands against the row's shape and makes its
// command, with the summary of it the event records. Each shape builds
// its summary's paths apart from its command's, so the record keeps the
// paths as typed whatever is later done with the command: the second
// copy is the event's own `[LAW:one-source-of-truth]` exception: the
// command's accessors hand out its slices to the bodies that read them,
// so the record cannot share them.
func (r Row) parse(operands []string, flags Flags) (Parsed, error) {
	switch o := r.Operands.(type) {
	case TakesPaths:
		if len(operands) == 0 {
			return Parsed{}, refuseUsage(fmt.Errorf("%s: %w; it takes one or more", r.Name, errNoPaths))
		}
		paths, asked := make([]PathText, len(operands)), make([]PathText, len(operands))
		for i, w := range operands {
			paths[i], asked[i] = PathText(w), PathText(w)
		}
		return Parsed{Asked: Summary{Name: r.Name, Paths: asked}, Command: o.Make(paths, flags)}, nil
	case TakesPairs:
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

// isGroup is whether word is a group's: the first of some row's two
// words.
func (c Commands) isGroup(word string) bool {
	return slices.ContainsFunc(c.rows, func(r Row) bool {
		g, in := r.Name.Group()
		return in && g == word
	})
}

// known names every command c holds, for a failure to list.
func (c Commands) known() string {
	names := make([]string, len(c.rows))
	for i, r := range c.rows {
		names[i] = r.Name.String()
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
