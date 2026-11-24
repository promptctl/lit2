package events

import (
	"fmt"
	"slices"

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

// message is a message's declaration: its name, as a refusal says it, its
// fields, and build, which makes the value a decoder answers from the
// fields read and the fields this binary does not know. It is the one
// statement of a message's fields `[LAW:one-source-of-truth]`: the decoder
// reads by it, and the tests derive every repeat and omission from it.
type message[T, R any] struct {
	name   string
	fields []field[T]
	build  func(*T, wire.Unknown) (R, error)
}

// declaration is a message's declaration as the tests read it, free of the
// type it reads into, so one registry holds every message.
type declaration struct {
	name   string
	fields []shape
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

// declare is the message name holding fields, built by build, and
// registers it, so no decoder reads a message the tests do not hold to its
// declaration. Two messages of one name, or one tag declared twice, is a
// build fault.
func declare[T, R any](name string, fields []field[T], build func(*T, wire.Unknown) (R, error)) *message[T, R] {
	d := declaration{name: name}
	for _, f := range fields {
		if slices.ContainsFunc(d.fields, func(s shape) bool { return s.tag == f.tag }) {
			panic(fmt.Errorf("events: %s's field %d: %w", name, f.tag, errRegisteredTwice))
		}
		d.fields = append(d.fields, shape{tag: f.tag, what: f.what, card: f.card})
	}
	if slices.ContainsFunc(declarations, func(o declaration) bool { return o.name == name }) {
		panic(fmt.Errorf("events: the message %s: %w", name, errRegisteredTwice))
	}
	m := &message[T, R]{name: name, fields: fields, build: build}
	d.read = func(b []byte) error { _, err := m.read(b); return err }
	declarations = append(declarations, d)
	return m
}

// decode reads r's fields by m's declaration. It is the one reader of a
// message's fields `[LAW:single-enforcer]`: a field held once that is given
// again is refused, since protobuf keeps the last of a singular field and a
// signed message read as its second value says what its writer never
// wrote; a field the message cannot be without that is absent is refused;
// and a field m does not declare, a newer binary's, is kept in r.Rest().
func (m *message[T, R]) decode(r *wire.Reader) (R, error) {
	var into T
	var zero R
	held := make([]bool, len(m.fields))
	err := r.Each(func(tag uint32, r *wire.Reader) error {
		i := slices.IndexFunc(m.fields, func(f field[T]) bool { return f.tag == tag })
		if i < 0 {
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
