package field

import (
	"fmt"
	"strings"
)

// Literal is a value as a command gives it, before any field's kind reads
// it: a scalar, a list, or an object (path-addressed-cli.md "Values").
// What a field accepts of these is its kind's and its row's to say.
//
//sumtype:decl
type Literal interface{ literal() }

// Scalar is one value's text.
type Scalar struct{ Text string }

// List is `[a, b]`: its items in order.
type List struct{ Items []Literal }

// Object is `{k: v, k: v}`: its pairs in order, each key once.
type Object struct{ Pairs []Pair }

// Pair is one key of an object and its value.
type Pair struct {
	Key   string
	Value Literal
}

func (Scalar) literal() {}
func (List) literal()   {}
func (Object) literal() {}

// ParseLiteral reads one command value. A value that opens with `[`, `{`
// or `"` is read by the grammar to its end; any other is a bare word, the
// whole of the text, so `add x/comments "a note"` takes what the shell
// passed it verbatim. Inside a list or an object, a bare word runs to a
// space, a comma, a colon, a bracket or a brace; items and pairs are
// separated by commas or spaces, a last comma may close them, and an
// object names each key once `[LAW:parse-dont-validate]`.
func ParseLiteral(text string) (Literal, error) {
	if text == "" || !strings.ContainsRune(`[{"`, rune(text[0])) {
		return Scalar{Text: text}, nil
	}
	r := reader{s: text}
	v, err := r.value()
	if err != nil {
		return nil, fmt.Errorf("%q is not a value: %w", text, err)
	}
	if r.skip(); r.i < len(r.s) {
		return nil, fmt.Errorf("%q is not a value: %s follows it", text, r.s[r.i:])
	}
	return v, nil
}

// reader is a cursor over a literal's text.
type reader struct {
	s string
	i int
}

// skip passes spaces.
func (r *reader) skip() {
	for r.i < len(r.s) && strings.ContainsRune(" \t\n\r", rune(r.s[r.i])) {
		r.i++
	}
}

func (r *reader) value() (Literal, error) {
	r.skip()
	if r.i >= len(r.s) {
		return nil, fmt.Errorf("a value is missing at the end")
	}
	switch r.s[r.i] {
	case '[':
		r.i++
		var items []Literal
		err := r.each(']', func() error {
			v, err := r.value()
			items = append(items, v)
			return err
		})
		return List{Items: items}, err
	case '{':
		r.i++
		var pairs []Pair
		seen := map[string]bool{}
		err := r.each('}', func() error {
			key, err := r.word()
			if err != nil {
				return err
			}
			if r.skip(); r.i >= len(r.s) || r.s[r.i] != ':' {
				return fmt.Errorf("the key %s has no colon after it", key)
			}
			r.i++
			if seen[key] {
				return fmt.Errorf("the key %s is given twice", key)
			}
			seen[key] = true
			v, err := r.value()
			pairs = append(pairs, Pair{Key: key, Value: v})
			return err
		})
		return Object{Pairs: pairs}, err
	case '"':
		s, err := r.quoted()
		return Scalar{Text: s}, err
	}
	w, err := r.word()
	return Scalar{Text: w}, err
}

// each reads the entries of a list or an object up to close: each entry,
// then a comma, a space or close. An empty entry is refused, so `[a,,b]`
// and `[,]` say nothing a person meant.
func (r *reader) each(close byte, entry func() error) error {
	for {
		r.skip()
		if r.i >= len(r.s) {
			return fmt.Errorf("it has no closing %c", close)
		}
		if r.s[r.i] == close {
			r.i++
			return nil
		}
		if r.s[r.i] == ',' {
			return fmt.Errorf("an entry is missing before a comma")
		}
		if err := entry(); err != nil {
			return err
		}
		if r.skip(); r.i < len(r.s) && r.s[r.i] == ',' {
			r.i++
		}
	}
}

// word reads a bare word: non-empty, to the first space, comma, colon,
// bracket, brace or quote.
func (r *reader) word() (string, error) {
	start := r.i
	for r.i < len(r.s) && !strings.ContainsRune(" \t\n\r,:[]{}\"", rune(r.s[r.i])) {
		r.i++
	}
	if r.i == start {
		return "", fmt.Errorf("a value is a bare word or a quoted string, not %q", r.s[r.i:r.i+1])
	}
	return r.s[start:r.i], nil
}

// quoted reads a quoted string, in which "" is a quote and every other
// character is itself, as an atom's quoted value is.
func (r *reader) quoted() (string, error) {
	v, tail, err := value(r.s[r.i:], " \t\n\r,:]}")
	if err != nil {
		return "", err
	}
	r.i = len(r.s) - len(tail)
	return v, nil
}
