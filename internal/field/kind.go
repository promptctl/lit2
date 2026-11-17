package field

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Kind is what a field holds. Parse is the one value parser for it: a raw
// value crosses here once, and what comes back is its canonical text
// `[LAW:parse-dont-validate]`.
type Kind interface {
	isKind()
	// Name is the kind's word in a workspace file.
	Name() string
	Parse(raw string) (string, error)
}

// Text is any non-empty line of text.
type Text struct{}

// Number is a finite decimal number, canonical as strconv writes it.
type Number struct{}

// Date is a calendar day, YYYY-MM-DD, canonical in that form.
type Date struct{}

// Enum is one of a fixed set of values, distinct and in declaration order.
type Enum struct{ values []string }

// Pattern is text that matches a regular expression. It keeps its source,
// which is what a workspace file says and what a reader prints.
type Pattern struct {
	src string
	re  *regexp.Regexp
}

func (Text) isKind()    {}
func (Number) isKind()  {}
func (Date) isKind()    {}
func (Enum) isKind()    {}
func (Pattern) isKind() {}

func (Text) Name() string    { return "text" }
func (Number) Name() string  { return "number" }
func (Date) Name() string    { return "date" }
func (Enum) Name() string    { return "enum" }
func (Pattern) Name() string { return "pattern" }

// Parse reads a text value: anything but nothing.
func (Text) Parse(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("a text value is not empty")
	}
	return raw, nil
}

// Parse reads a number. NaN and infinities are not numbers a person writes.
func (Number) Parse(raw string) (string, error) {
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("%q is not a finite number", raw)
	}
	return strconv.FormatFloat(f, 'f', -1, 64), nil
}

// Parse reads a calendar day.
func (Date) Parse(raw string) (string, error) {
	t, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return "", fmt.Errorf("%q is not a date: YYYY-MM-DD", raw)
	}
	return t.Format(time.DateOnly), nil
}

// Parse reads one of the enum's values.
func (e Enum) Parse(raw string) (string, error) {
	if !slices.Contains(e.values, raw) {
		return "", fmt.Errorf("%q is not one of %s", raw, strings.Join(e.values, ", "))
	}
	return raw, nil
}

// Parse reads text the pattern matches.
func (p Pattern) Parse(raw string) (string, error) {
	if !p.re.MatchString(raw) {
		return "", fmt.Errorf("%q does not match %s", raw, p.src)
	}
	return raw, nil
}

// Values is the enum's declared values, in declaration order.
func (e Enum) Values() []string { return slices.Clone(e.values) }

// NewEnum declares an enum. It refuses no values and a repeated one, the two
// declarations that would make a Parse meaningless.
func NewEnum(values []string) (Enum, error) {
	if len(values) == 0 {
		return Enum{}, fmt.Errorf("an enum declares at least one value")
	}
	seen := map[string]bool{}
	for _, v := range values {
		if v == "" {
			return Enum{}, fmt.Errorf("an enum value is not empty")
		}
		if seen[v] {
			return Enum{}, fmt.Errorf("%q is declared twice", v)
		}
		seen[v] = true
	}
	return Enum{values: slices.Clone(values)}, nil
}

// NewPattern compiles a declared pattern. A pattern is the shape of a whole
// value, so it matches anchored at both ends whether or not src says so.
func NewPattern(src string) (Pattern, error) {
	if _, err := regexp.Compile(src); err != nil {
		return Pattern{}, fmt.Errorf("%q is not a regular expression: %w", src, err)
	}
	// src compiles alone, so wrapping it cannot close a group it opened.
	re := regexp.MustCompile(`^(?:` + src + `)$`)
	return Pattern{src: src, re: re}, nil
}

// Source is the pattern as the workspace file wrote it.
func (p Pattern) Source() string { return p.src }

// KindNamed reads a kind's word. Only the five kinds exist; a kind that needs
// parameters is built by the caller that has them, so a bare word here can
// only name a parameterless kind.
func KindNamed(word string) (Kind, bool) {
	switch word {
	case "text":
		return Text{}, true
	case "number":
		return Number{}, true
	case "date":
		return Date{}, true
	}
	return nil, false
}
