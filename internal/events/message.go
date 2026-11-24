package events

import (
	"fmt"
	"slices"
	"strings"

	"github.com/promptctl/lit2/internal/events/wire"
)

// cardinality is how many times a message holds a field.
type cardinality uint8

const (
	// one is a field the message cannot be without, held once.
	one cardinality = iota + 1
	// optional is a field the message holds once or not at all.
	optional
	// many is a list, each element its own field, in stored order.
	many
	// alternative is one of the forms of the message's sum: held once or
	// not at all, and counted against the sum.
	alternative
)

// field is one field of a message, declared once: its tag, the part a
// refusal names it by, how many times the message holds it, and its reader,
// which reads the field's value into the message being read.
type field[T any] struct {
	tag  uint32
	what part
	card cardinality
	read func(*T, *wire.Reader) error
}

// forms is a sum a message holds: the part a refusal names the thing its
// forms are of, how many of the forms this binary knows the message holds,
// and the forms' tags, which declare reads off the fields declared alternative, so
// a form is stated once, on its field `[LAW:one-source-of-truth]`. One
// holds exactly one.
// Optional holds one or none, and none is a form a newer binary wrote,
// which the message holds as a field this binary does not know; a message
// holding no field at all for its thing is refused either way. Each form is
// itself a field held at most once, so a form given twice is that field
// twice.
type forms struct {
	what part
	card cardinality
	tags []uint32
}

// refusal is the sum's refusal of a message holding the forms held, and
// newer when it holds a field this binary does not know: the one check of
// a sum `[LAW:single-enforcer]`, so no build counts its forms.
func (s forms) refusal(held []part, newer bool) error {
	switch {
	case len(held) > 1:
		names := make([]string, len(held))
		for i, p := range held {
			names[i] = p.String()
		}
		return fmt.Errorf("its %s: %w", strings.Join(names, " and "), notOneForm{What: s.what})
	case len(held) == 0 && (s.card != optional || !newer):
		return fmt.Errorf("none of its forms: %w", missing{What: s.what})
	}
	return nil
}

// message is a message's declaration: its name, as a refusal says it, its
// fields, the sums among them, and build, which makes the value a decoder
// answers from the fields read and the fields this binary does not know. It
// is the one statement of a message's fields `[LAW:one-source-of-truth]`:
// the decoder reads by it, and the tests derive every repeat, omission and
// form from it.
type message[T, R any] struct {
	name   string
	fields []field[T]
	sums   []forms
	build  func(*T, wire.Unknown) (R, error)
}

// declaration is a message's declaration as the tests read it, free of the
// type it reads into, so one registry holds every message.
type declaration struct {
	name   string
	fields []shape
	sums   []forms
	read   func([]byte) error
}

// shape is a field as the tests read it: its tag, its part and how many
// times the message holds it.
type shape struct {
	tag  uint32
	what part
	card cardinality
}

// declarations is every message a decoder reads. declare is its one writer,
// as the package's variables are initialized, so it is whole before any
// decoder runs and never changes after `[LAW:no-shared-mutable-globals]`.
var declarations []declaration

// declare is the message name holding fields, among them the sums, built by
// build, and registers it, so no decoder reads a message the tests do not
// hold to its declaration. Two messages of one name, one tag declared
// twice, a message of more than one sum, a sum of fewer than two forms, or
// a form of no sum, is a build fault.
func declare[T, R any](name string, fields []field[T], build func(*T, wire.Unknown) (R, error), sums ...forms) *message[T, R] {
	d := declaration{name: name}
	var formTags []uint32
	for _, f := range fields {
		if slices.ContainsFunc(d.fields, func(s shape) bool { return s.tag == f.tag }) {
			panic(fmt.Errorf("events: %s's field %d: %w", name, f.tag, errRegisteredTwice))
		}
		d.fields = append(d.fields, shape{tag: f.tag, what: f.what, card: f.card})
		if f.card == alternative {
			formTags = append(formTags, f.tag)
		}
	}
	switch {
	case len(sums) > 1:
		panic(fmt.Errorf("events: %s declares %d sums; a message holds at most one", name, len(sums)))
	case len(sums) == 1 && len(formTags) < 2:
		panic(fmt.Errorf("events: %s's sum %s has %d forms; a sum has more than one", name, sums[0].what, len(formTags)))
	case len(sums) == 0 && len(formTags) > 0:
		panic(fmt.Errorf("events: %s declares forms of no sum", name))
	case len(sums) == 1:
		sums[0].tags = formTags
	}
	d.sums = sums
	if slices.ContainsFunc(declarations, func(o declaration) bool { return o.name == name }) {
		panic(fmt.Errorf("events: the message %s: %w", name, errRegisteredTwice))
	}
	m := &message[T, R]{name: name, fields: fields, sums: sums, build: build}
	d.read = func(b []byte) error { _, err := m.read(b); return err }
	declarations = append(declarations, d)
	return m
}

// decode reads r's fields by m's declaration. It is the one reader of a
// message's fields `[LAW:single-enforcer]`: a field held once that is given
// again is refused, since protobuf keeps the last of a singular field and a
// signed message read as its second value says what its writer never
// wrote; a field the message cannot be without that is absent is refused; a
// sum held in other than its declared forms is refused; and a field m does
// not declare, a newer binary's, is kept in r.Rest().
func (m *message[T, R]) decode(r *wire.Reader) (R, error) {
	var into T
	var zero R
	held := make([]bool, len(m.fields))
	newer := false
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		i := slices.IndexFunc(m.fields, func(f field[T]) bool { return f.tag == tag })
		if i < 0 {
			newer = true
			return nil
		}
		f := m.fields[i]
		if held[i] && f.card != many {
			return twice{What: f.what}
		}
		held[i] = true
		return f.read(&into, r)
	})
	for i, f := range m.fields {
		if err == nil && f.card == one && !held[i] {
			err = missing{What: f.what}
		}
	}
	for _, s := range m.sums {
		var in []part
		for i, f := range m.fields {
			if held[i] && slices.Contains(s.tags, f.tag) {
				in = append(in, f.what)
			}
		}
		if err == nil {
			err = s.refusal(in, newer)
		}
	}
	if err != nil {
		return zero, fmt.Errorf("%s: %w", m.name, err)
	}
	v, err := m.build(&into, r.Rest())
	if err != nil {
		return zero, fmt.Errorf("%s: %w", m.name, err)
	}
	return v, nil
}

// read decodes the message b.
func (m *message[T, R]) read(b []byte) (R, error) { return m.decode(wire.NewReader(b)) }

// whole is a build for a message that keeps no field it does not know: the
// fields read, as they are.
func whole[T any](t *T, _ wire.Unknown) (T, error) { return *t, nil }

// nameField is a name field: the one reader of a name in a message, so every
// message parses a name alike `[LAW:single-enforcer]`.
func nameField[T any](tag uint32, what part, card cardinality, into func(*T) *Name) field[T] {
	return field[T]{tag, what, card, func(t *T, r *wire.Reader) error {
		n, err := ParseName(string(r.Bytes()))
		if err != nil {
			return fmt.Errorf("its %s: %w", what, err)
		}
		*into(t) = n
		return nil
	}}
}

// fixedField is a field of a fixed number of bytes, the length of into's slice,
// which it fills.
func fixedField[T any](tag uint32, what part, card cardinality, into func(*T) []byte) field[T] {
	return field[T]{tag, what, card, func(t *T, r *wire.Reader) error {
		b, dst := r.Bytes(), into(t)
		copy(dst, b)
		return sized(what, b, len(dst))
	}}
}

// idsField is a list of event ids, each element one id.
func idsField[T any](tag uint32, what part, card cardinality, into func(*T) *[]EventID) field[T] {
	return field[T]{tag, what, card, func(t *T, r *wire.Reader) error {
		id, err := ReadEventID(r.Bytes())
		*into(t) = append(*into(t), id)
		return err
	}}
}
