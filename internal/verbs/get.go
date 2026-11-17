package verbs

import (
	"fmt"
	"strings"

	"github.com/promptctl/lit2/internal/address"
	"github.com/promptctl/lit2/internal/config"
	"github.com/promptctl/lit2/internal/events"
	"github.com/promptctl/lit2/internal/field"
)

// Got is a read: each path's result, and when none was refused, what it
// prints.
type Got struct {
	Operands []Result
	Out      string
}

// Refusal is the first path's refusal, and false when none was refused.
func (g Got) Refusal() (Refused, bool) { return firstRefused(g.Operands) }

// line is one value a read prints, under the path it is at.
type line struct {
	at    string
	value string
}

// Get reads the nodes at paths. One path naming one value prints it bare;
// a node prints `$ = <node>` and a line per value it holds, each under its
// path from `$`; several paths print each value under its path. A value a
// reader could not tell the end of prints quoted.
func Get(w World, paths []string) Got {
	var g Got
	var reads [][]line
	var whole []bool
	for _, p := range paths {
		a, r := address.Parse(w.State, w.Schema, p)
		if r != nil {
			g.Operands = append(g.Operands, Refused{Text: p, Reason: r.Reason(), Err: r, Detail: r.Error()})
			continue
		}
		ls := read(w, a)
		g.Operands = append(g.Operands, Accepted{Text: p, Canon: a.String(), Values: len(ls)})
		reads = append(reads, ls)
		_, isWhole := a.Target.(address.Whole)
		whole = append(whole, isWhole)
	}
	if _, refused := g.Refusal(); refused {
		return g
	}
	var b strings.Builder
	for i, ls := range reads {
		single := len(reads) == 1
		if single && !whole[i] && len(ls) == 1 {
			b.WriteString(ls[0].value + "\n")
			continue
		}
		canon := g.Operands[i].(Accepted).Canon
		if single && whole[i] {
			fmt.Fprintf(&b, "$ = %s\n", canon)
		}
		for _, l := range ls {
			at := l.at
			if single && whole[i] {
				at = "$" + strings.TrimPrefix(at, strings.TrimSuffix(canon, "/"))
			}
			fmt.Fprintf(&b, "%s\t%s\n", at, shown(l.value, false))
		}
	}
	g.Out = b.String()
	return g
}

// read is the lines the address holds, each at its canonical path.
func read(w World, a address.Address) []line {
	base := strings.TrimSuffix(a.String(), "/")
	switch t := a.Target.(type) {
	case address.Whole:
		var out []line
		node := base
		for _, r := range w.Schema.Table().Rows(a.Place) {
			out = append(out, rowLines(w, a, node, r.Path(), r)...)
		}
		return out
	case address.Field:
		return rowLines(w, a, strings.TrimSuffix(base, "/"+string(t.Path)), t.Path, t.Row)
	case address.Element:
		switch k := t.Key.(type) {
		case address.Member:
			for _, v := range Read(w, a.Node, a.Place, t.Path, config.FieldRow{At: t.Path, Decl: t.Decl}) {
				if v == k.Name.String() {
					return []line{{at: base, value: v}}
				}
			}
			return nil
		case address.ElementKey:
			for _, e := range elements(w.item(a.Node.(address.Item).ID), events.MustName(string(t.Path))) {
				if e.key == k.Rendered {
					return []line{{at: base, value: fmt.Sprintf("[%s] %s", e.by, e.text)}}
				}
			}
		}
	}
	return nil
}

// rowLines is one row's values on a node, each at the row's path, and a
// list's elements each at its own key.
func rowLines(w World, a address.Address, node string, path field.Path, r config.Row) []line {
	at := node + "/" + string(path)
	if f, ok := r.(config.FieldRow); ok && f.Decl.Multi == field.Elements {
		var out []line
		for _, e := range elements(w.item(a.Node.(address.Item).ID), events.MustName(string(path))) {
			out = append(out, line{at: at + "/" + e.key, value: fmt.Sprintf("[%s] %s", e.by, e.text)})
		}
		return out
	}
	var out []line
	for _, v := range Read(w, a.Node, a.Place, path, r) {
		out = append(out, line{at: at, value: v})
	}
	return out
}
