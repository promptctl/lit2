package field

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Atom is one test over one path of an item: `p`, `!p`, `p=v`, `p!=v` or
// `p~re`, where `v` may be several values, `p=a,b`. Requirements read it, and so will a CLI selector: the one grammar
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

// Equals holds when some value the item holds is one of Values.
type Equals struct{ Values []string }

// NotEquals holds when no value the item holds is one of Values.
type NotEquals struct{ Values []string }

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

// Holds evaluates an atom over what the item holds at its path, whose field
// is of kind k. This is the one evaluation of an atom: a requirement and a
// selector both reach it `[LAW:single-enforcer]`.
func (a Atom) Holds(k Kind, f Found) Truth {
	switch op := a.op.(type) {
	case Present:
		return Truth(len(f) > 0)
	case Absent:
		return Truth(len(f) == 0)
	case Equals:
		return Truth(contains(f, oneOf(k, op.Values)))
	case NotEquals:
		return !Truth(contains(f, oneOf(k, op.Values)))
	case Matches:
		return Truth(contains(f, op.Re.MatchString))
	}
	panic(fmt.Sprintf("field: %T is not an Op", a.op))
}

// oneOf is whether a stored value is one of values, compared as k writes
// them: an item stores a value in its kind's canonical form, so `est=1.0` meets
// the 1 a number field stores. A value k does not parse is one no item of that
// kind holds.
func oneOf(k Kind, values []string) func(string) bool {
	canonical := make([]string, 0, len(values))
	for _, v := range values {
		if c, err := k.Parse(v); err == nil {
			canonical = append(canonical, c)
		}
	}
	return func(stored string) bool { return slices.Contains(canonical, stored) }
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
// operator with its values, which are the rest of the text.
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
	if opWord == "~" {
		pattern, err := only(rest)
		if err != nil {
			return Atom{}, fmt.Errorf("%q is not an atom: %w", s, err)
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return Atom{}, fmt.Errorf("%q is not an atom: %q is not a regular expression: %w", s, pattern, err)
		}
		return Atom{path: path, op: Matches{Re: re}, src: s}, nil
	}
	values, err := list(rest)
	if err != nil {
		return Atom{}, fmt.Errorf("%q is not an atom: %w", s, err)
	}
	if opWord == "=" {
		return Atom{path: path, op: Equals{Values: values}, src: s}, nil
	}
	return Atom{path: path, op: NotEquals{Values: values}, src: s}, nil
}

// list reads the values of = and !=: one or more, separated by commas.
func list(rest string) ([]string, error) {
	var out []string
	for {
		v, tail, err := value(rest, ",")
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		if tail == "" {
			return out, nil
		}
		rest = tail[1:]
	}
}

// only reads the one value of ~, a regular expression, which may hold commas.
func only(rest string) (string, error) {
	v, tail, err := value(rest, "")
	if err != nil {
		return "", err
	}
	if tail != "" {
		return "", fmt.Errorf("%s follows the quoted value", tail)
	}
	return v, nil
}

// value reads one value from the front of s, and returns what follows it,
// which is empty or starts at one of stops. A value is a quoted string, in
// which "" is a literal quote and every other character is itself, a backslash
// included, so a regex or a path needs no escaping; or a bare word, non-empty,
// with no space or quote, running to the first of stops.
func value(s, stops string) (string, string, error) {
	if strings.HasPrefix(s, `"`) {
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			if s[i] != '"' {
				b.WriteByte(s[i])
				continue
			}
			if i+1 < len(s) && s[i+1] == '"' {
				b.WriteByte('"')
				i++
				continue
			}
			tail := s[i+1:]
			if tail != "" && !strings.ContainsRune(stops, rune(tail[0])) {
				return "", "", fmt.Errorf("%s follows the quoted value", tail)
			}
			return b.String(), tail, nil
		}
		return "", "", fmt.Errorf("the quoted value %s has no closing quote", s)
	}
	end := len(s)
	if stops != "" {
		if i := strings.IndexAny(s, stops); i >= 0 {
			end = i
		}
	}
	word := s[:end]
	if word == "" || strings.ContainsAny(word, " \t\"") {
		return "", "", fmt.Errorf("a value is a bare word or a quoted string")
	}
	return word, s[end:], nil
}
