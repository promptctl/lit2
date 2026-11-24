package field

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/promptctl/lit2/internal/events"
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

// The refusals of a raw value its kind does not read, and of a declared
// pattern that does not compile, each wrapped where it is refused so the
// message still says what was refused. ErrNotOneOf is exported so a caller
// that holds an Enum only as a Kind, a config row's, can identify its refusal.
var (
	errNotAnItem  = errors.New("invalid item")
	ErrNotOneOf   = errors.New("a value outside the enum")
	errNoMatch    = errors.New("a value the pattern does not match")
	errNotARegexp = errors.New("invalid regular expression")
)

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

// The engine kinds hold what the engine names, so no workspace file
// declares one.

// Column is a column's name. Which columns an item may enter is its
// resolved graph's to say, at the write.
type Column struct{}

// ItemRef is an item, as its path renders it.
type ItemRef struct{}

// Level is a level, 1 to 5.
type Level struct{}

// Principal is a principal, as its did:key.
type Principal struct{}

// Role is a role's name. Which roles exist is the policy's to say, at the
// write.
type Role struct{}

// Threshold is how many owners a change of the owners needs: majority, or
// a number.
type Threshold struct{}

// Digest is a proposal's digest, or a prefix of its hex that names one.
// Which proposals exist is the fold's to say, at the write.
type Digest struct{}

func (Text) isKind()      {}
func (Number) isKind()    {}
func (Date) isKind()      {}
func (Enum) isKind()      {}
func (Pattern) isKind()   {}
func (Column) isKind()    {}
func (ItemRef) isKind()   {}
func (Level) isKind()     {}
func (Principal) isKind() {}
func (Role) isKind()      {}
func (Threshold) isKind() {}
func (Digest) isKind()    {}

func (Text) Name() string      { return "text" }
func (Number) Name() string    { return "number" }
func (Date) Name() string      { return "date" }
func (Enum) Name() string      { return "enum" }
func (Pattern) Name() string   { return "pattern" }
func (Column) Name() string    { return "column" }
func (ItemRef) Name() string   { return "item" }
func (Level) Name() string     { return "level" }
func (Principal) Name() string { return "principal" }
func (Role) Name() string      { return "role" }
func (Threshold) Name() string { return "threshold" }
func (Digest) Name() string    { return "digest" }

// Parse reads a principal's did:key.
func (Principal) Parse(raw string) (string, error) {
	p, err := events.ParsePrincipalID(raw)
	if err != nil {
		return "", err
	}
	return p.String(), nil
}

// Parse reads a role's name.
func (Role) Parse(raw string) (string, error) {
	n, err := events.ParseName(raw)
	if err != nil {
		return "", fmt.Errorf("a role: %w", err)
	}
	return n.String(), nil
}

// Parse reads a threshold.
func (Threshold) Parse(raw string) (string, error) {
	t, err := events.ParseThreshold(raw)
	if err != nil {
		return "", err
	}
	return events.ThresholdText(t), nil
}

// Parse reads a digest's hex, whole or a prefix of it.
func (Digest) Parse(raw string) (string, error) {
	if raw == "" || len(raw) > 64 || strings.Trim(raw, "0123456789abcdef") != "" {
		return "", fmt.Errorf("%q is not a digest: its hex, or a prefix of it", raw)
	}
	return raw, nil
}

// Parse reads a column's name.
func (Column) Parse(raw string) (string, error) {
	n, err := events.ParseName(raw)
	if err != nil {
		return "", fmt.Errorf("a column: %w", err)
	}
	return n.String(), nil
}

// Parse reads an item as a path renders it: one word.
func (ItemRef) Parse(raw string) (string, error) {
	if raw == "" || strings.ContainsAny(raw, " \t\n\r\"") {
		return "", fmt.Errorf("%q: %w; an item is an id, as its path renders it", raw, errNotAnItem)
	}
	return raw, nil
}

// Parse reads a level.
func (Level) Parse(raw string) (string, error) {
	n, err := strconv.Atoi(raw)
	if err != nil {
		return "", fmt.Errorf("%q is not a level: %w", raw, events.ErrNotALevel)
	}
	if _, err := events.ParseLevel(n); err != nil {
		return "", err
	}
	return strconv.Itoa(n), nil
}

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
		return "", fmt.Errorf("%q: %w; the values are %s", raw, ErrNotOneOf, strings.Join(e.values, ", "))
	}
	return raw, nil
}

// Parse reads text the pattern matches.
func (p Pattern) Parse(raw string) (string, error) {
	if !p.re.MatchString(raw) {
		return "", fmt.Errorf("%q: %w; the pattern is %s", raw, errNoMatch, p.src)
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
		return Pattern{}, fmt.Errorf("%q: %w: %w", src, errNotARegexp, err)
	}
	// src compiles alone, so wrapping it cannot close a group it opened.
	re := regexp.MustCompile(`^(?:` + src + `)$`)
	return Pattern{src: src, re: re}, nil
}

// Source is the pattern as the workspace file wrote it.
func (p Pattern) Source() string { return p.src }

// KindNamed reads a kind's word. A workspace declares only the five kinds,
// and the engine's own kinds have no word; a kind that needs parameters is
// built by the caller that has them, so a bare word here can only name a
// parameterless kind.
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
