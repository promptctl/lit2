package field

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Atom is one test over one path of an item: `p`, `!p`, `p=v`, `p!=v` or
// `p~re`. Requirements read it, and so will a CLI selector: the one grammar
// `[LAW:one-source-of-truth]`.
type Atom struct {
	path Path
	op   Op
	src  string
}

// Op is the operator of an atom.
type Op interface{ isOp() }

// Present holds when the path has at least one value.
type Present struct{}

// Absent holds when the path has none.
type Absent struct{}

// Equals holds when some value is V.
type Equals struct{ V string }

// NotEquals holds when no value is V.
type NotEquals struct{ V string }

// Matches holds when some value matches Re.
type Matches struct{ Re *regexp.Regexp }

func (Present) isOp()   {}
func (Absent) isOp()    {}
func (Equals) isOp()    {}
func (NotEquals) isOp() {}
func (Matches) isOp()   {}

// Path is the path the atom reads.
func (a Atom) Path() Path { return a.path }

// Op is the atom's operator.
func (a Atom) Op() Op { return a.op }

// String is the atom as the source wrote it, which is how an unmet test is
// named in a refusal and on a read.
func (a Atom) String() string { return a.src }

// Truth is whether an atom holds on an item.
type Truth bool

// Found is what an item holds at a path: its values, one for a scalar and
// many for a list. Nothing held is an empty Found, which is absence.
type Found []string

// Holds evaluates an atom over what the item holds at its path. This is the
// one evaluation of an atom: a requirement and a selector both reach it
// `[LAW:single-enforcer]`.
func (a Atom) Holds(f Found) Truth {
	switch op := a.op.(type) {
	case Present:
		return Truth(len(f) > 0)
	case Absent:
		return Truth(len(f) == 0)
	case Equals:
		return Truth(contains(f, func(v string) bool { return v == op.V }))
	case NotEquals:
		return !Truth(contains(f, func(v string) bool { return v == op.V }))
	case Matches:
		return Truth(contains(f, op.Re.MatchString))
	}
	panic(fmt.Sprintf("field: %T is not an Op", a.op))
}

func contains(f Found, hit func(string) bool) bool {
	for _, v := range f {
		if hit(v) {
			return true
		}
	}
	return false
}

// segment is one step of a path: a lowercase word, with digits, - and _.
const segment = `[a-z][a-z0-9_-]*`

// atomGrammar is the whole grammar: an optional !, a path, and at most one
// operator with its value. A value is the rest of the text, bare or quoted.
var atomGrammar = regexp.MustCompile(`^(!?)(` + segment + `(?:/` + segment + `)*)(?:(!=|=|~)(.*))?$`)

// ParseAtom reads an atom from its source. It refuses what the grammar does
// not say and a regular expression that does not compile; whether the path is
// declared and the value fits its kind is the config's to say.
func ParseAtom(s string) (Atom, error) {
	m := atomGrammar.FindStringSubmatch(s)
	if m == nil {
		return Atom{}, fmt.Errorf("%q is not an atom: p, !p, p=v, p!=v or p~re", s)
	}
	neg, path, opWord, rest := m[1], Path(m[2]), m[3], m[4]
	if opWord == "" {
		if neg == "" {
			return Atom{path: path, op: Present{}, src: s}, nil
		}
		return Atom{path: path, op: Absent{}, src: s}, nil
	}
	if neg != "" {
		return Atom{}, fmt.Errorf("%q is not an atom: ! negates a path alone, write !%s%s%s", s, path, opWord, rest)
	}
	value, err := atomValue(rest)
	if err != nil {
		return Atom{}, fmt.Errorf("%q is not an atom: %w", s, err)
	}
	switch opWord {
	case "=":
		return Atom{path: path, op: Equals{V: value}, src: s}, nil
	case "!=":
		return Atom{path: path, op: NotEquals{V: value}, src: s}, nil
	}
	re, err := regexp.Compile(value)
	if err != nil {
		return Atom{}, fmt.Errorf("%q is not an atom: %q is not a regular expression: %w", s, value, err)
	}
	return Atom{path: path, op: Matches{Re: re}, src: s}, nil
}

// atomValue is the value an atom names: a quoted string unquoted, or a bare
// word, which is non-empty and has no space or quote in it.
func atomValue(rest string) (string, error) {
	if strings.HasPrefix(rest, `"`) {
		v, err := strconv.Unquote(rest)
		if err != nil {
			return "", fmt.Errorf("the quoted value %s is not a string", rest)
		}
		return v, nil
	}
	if rest == "" || strings.ContainsAny(rest, " \t\"") {
		return "", fmt.Errorf("a value is a bare word or a quoted string")
	}
	return rest, nil
}
