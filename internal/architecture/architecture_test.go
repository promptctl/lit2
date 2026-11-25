// Package architecture holds every import in the tree to ARCHITECTURE.md:
// its layers block is the one source of the dependency order and this test
// is its one enforcer `[LAW:one-way-deps]` `[LAW:single-enforcer]` (D5).
package architecture

import (
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"unicode"
)

// rule is what a violation breaks; a test finds a violation by its rule.
type rule string

const (
	upward     rule = "imports a unit not below it in the layers block"
	unlisted   rule = "is under internal/ and is not a unit in the layers block"
	undeclared rule = "imports a module the dependencies block does not give its unit"
	program    rule = "is the program and imports a unit other than root, or its test and imports one that is neither root nor a test kit"
	ambient    rule = "reads the process's world outside root"
	exits      rule = "calls os.Exit outside cmd/lit2"
	embeds     rule = "embeds a file outside internal/assets, which holds every shipped file"
	network    rule = "imports the network, which lit2 never speaks; the user's git does (decision G, D13)"
	missingRow rule = "has no row in ARCHITECTURE.md's concern table"
	rowUnit    rule = "has a concern row naming no unit, or one not in the layers block"
	drawing    rule = "is not the layers block's order from cli to git"
	noBlock    rule = "is missing from ARCHITECTURE.md"
	opened     rule = "opens an envelope below the verbs, which no layer there may read (INV:opaque-envelope)"
	reaches    rule = "reaches the disk, git or a process from a test without the integration build constraint, outside a port's contract suite and the factories it is given (D2)"
)

// readers is the units that may open an envelope or name the keyring that
// does, its type or its constructor: events, which defines them, the root,
// which reads the keys, the verbs, which seal and open prose, and cli,
// which renders it (§3.19).
// This test is INV:opaque-envelope's one enforcer `[LAW:single-enforcer]`.
var readers = []string{"events", "root", "verbs", "cli"}

// guarded is the events names that open an envelope: Open and the keyring
// it reads with, its type and its constructor.
var guarded = []string{"Open", "Keyring", "NewKeyring"}

type violation struct {
	where string
	rule  rule
}

func (v violation) String() string { return v.where + " " + string(v.rule) }

// check reads the module at the root of tree and names every way it breaks
// ARCHITECTURE.md.
func check(tree fs.FS) ([]violation, error) {
	mod, err := modulePath(tree)
	if err != nil {
		return nil, err
	}
	note, err := fs.ReadFile(tree, "ARCHITECTURE.md")
	if err != nil {
		return nil, err
	}
	design, err := fs.ReadFile(tree, "design-docs/architecture.md")
	if err != nil {
		return nil, err
	}
	var vs []violation
	layers, ok := fenced(string(note), "layers")
	if !ok {
		vs = append(vs, violation{"the layers block", noBlock})
	}
	deps, ok := fenced(string(note), "dependencies")
	if !ok {
		vs = append(vs, violation{"the dependencies block", noBlock})
	}
	rank := map[string]int{}
	for i, unit := range layers {
		rank[unit] = i
	}
	declared := map[string]string{}
	for _, line := range deps {
		module, unit, _ := strings.Cut(line, " ")
		declared[module] = strings.TrimSpace(unit)
	}
	code, err := checkCode(tree, mod, rank, declared)
	if err != nil {
		return nil, err
	}
	vs = append(vs, code...)
	vs = append(vs, checkConcerns(string(note), string(design), rank)...)
	return append(vs, checkDrawing(string(design), layers)...), nil
}

func modulePath(tree fs.FS) (string, error) {
	b, err := fs.ReadFile(tree, "go.mod")
	if err != nil {
		return "", err
	}
	for line := range strings.Lines(string(b)) {
		if mod, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return mod, nil
		}
	}
	return "", fmt.Errorf("go.mod names no module")
}

// fenced is the lines of the fenced block whose info string is info.
func fenced(text, info string) ([]string, bool) {
	var block []string
	in, found := false, false
	for line := range strings.Lines(text) {
		line = strings.TrimRight(line, "\n")
		switch {
		case !in && line == "```"+info:
			in, found = true, true
		case in && line == "```":
			return block, found
		case in && strings.TrimSpace(line) != "":
			block = append(block, strings.TrimSpace(line))
		}
	}
	return block, found && !in
}

// source is one .go file of the tree, parsed.
type source struct {
	path string
	// owner is the unit the file is in, or cmd for the program
	owner string
	// pkg is the import path of the file's directory
	pkg  string
	f    *ast.File
	test bool
	// gated is whether the file's build constraint holds it out of every
	// build without the integration tag
	gated bool
	// imported is the name the file uses for each import, to its path
	imported map[string]string
}

func checkCode(tree fs.FS, mod string, rank map[string]int, declared map[string]string) ([]violation, error) {
	var vs []violation
	units, err := fs.ReadDir(tree, "internal")
	if err != nil {
		return nil, err
	}
	for _, u := range units {
		if _, ok := rank[u.Name()]; !ignored(u.Name()) && (!ok || !u.IsDir()) {
			vs = append(vs, violation{"internal/" + u.Name(), unlisted})
		}
	}
	fset := token.NewFileSet()
	var srcs []*source
	for _, top := range []string{"cmd", "internal"} {
		err := fs.WalkDir(tree, top, func(path string, d fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case d.IsDir() && ignored(d.Name()):
				return fs.SkipDir
			case d.IsDir() || ignored(d.Name()) || !strings.HasSuffix(path, ".go"):
				return nil
			}
			src, err := fs.ReadFile(tree, path)
			if err != nil {
				return err
			}
			f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution|parser.ParseComments)
			if err != nil {
				return err
			}
			owner := strings.Split(path, "/")[1]
			if top == "cmd" {
				owner = "cmd"
			}
			s := &source{path: path, owner: owner, pkg: mod + "/" + filepath.ToSlash(filepath.Dir(path)), f: f,
				test: strings.HasSuffix(path, "_test.go"), gated: gated(f), imported: map[string]string{}}
			for _, imp := range f.Imports {
				ip := strings.Trim(imp.Path.Value, "`\"")
				s.imported[localName(imp, ip)] = ip
			}
			srcs = append(srcs, s)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	kits := testKits(srcs, mod)
	for _, s := range srcs {
		vs = append(vs, checkFile(s, mod, rank, declared, kits)...)
	}
	return append(vs, newModule(fset, srcs, mod, kits).reaching()...), nil
}

// testKits is every package of the module the program does not build:
// none is a package main or imported by a file the program builds, so
// tests, and kits, alone import it, as gittest and filestest are.
func testKits(srcs []*source, mod string) map[string]bool {
	built := map[string]bool{}
	for _, s := range srcs {
		built[s.pkg] = built[s.pkg] || !s.test && s.f.Name.Name == "main"
	}
	for grew := true; grew; {
		grew = false
		for _, s := range srcs {
			for _, ip := range s.imported {
				if !s.test && built[s.pkg] && strings.HasPrefix(ip, mod+"/") && !built[ip] {
					built[ip], grew = true, true
				}
			}
		}
	}
	kits := map[string]bool{}
	for _, s := range srcs {
		kits[s.pkg] = kits[s.pkg] || !s.test && !built[s.pkg]
	}
	return kits
}

// ignored is whether the go tool passes a file or directory by, as it does
// testdata and any name starting with . or _, so it is no part of the tree.
func ignored(name string) bool {
	return name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

func checkFile(s *source, mod string, rank map[string]int, declared map[string]string, kits map[string]bool) []violation {
	var vs []violation
	path, owner, f, test := s.path, s.owner, s.f, s.test
	for _, imp := range f.Imports {
		ip := strings.Trim(imp.Path.Value, "`\"")
		if imp.Name != nil && imp.Name.Name == "." && world[ip] != nil && !test && owner != "root" {
			vs = append(vs, violation{path + ": import . " + ip, ambient})
		}
		// a dot import hides events.Open from the selector check below
		if imp.Name != nil && imp.Name.Name == "." && ip == mod+"/internal/events" && !slices.Contains(readers, owner) {
			vs = append(vs, violation{path + ": import . " + ip, opened})
		}
		if ip == "net" || strings.HasPrefix(ip, "net/") || ip == "crypto/tls" {
			vs = append(vs, violation{path + ": import " + ip, network})
		}
		at := violation{path + ": import " + ip, ""}
		if rest, ok := strings.CutPrefix(ip, mod+"/internal/"); ok {
			unit := strings.Split(rest, "/")[0]
			to, listed := rank[unit]
			from, owned := rank[owner]
			switch {
			// the program's test drives the built binary, through a test kit
			case owner == "cmd" && unit != "root" && (!test || !kits[ip]):
				at.rule = program
			case owner != "cmd" && listed && owned && unit != owner && to <= from:
				at.rule = upward
			}
		} else if strings.Contains(strings.Split(ip, "/")[0], ".") && !declaredFor(declared, ip, owner) {
			at.rule = undeclared
		}
		if at.rule != "" {
			vs = append(vs, at)
		}
	}
	// a go:embed directive outside assets ships a file that assets does not
	// name, so the Asset handles would miss it `[LAW:one-source-of-truth]`.
	for _, group := range f.Comments {
		for _, c := range group.List {
			if strings.HasPrefix(c.Text, "//go:embed") && owner != "assets" {
				vs = append(vs, violation{path + ": " + c.Text, embeds})
			}
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		ip, isImport := s.imported[x.Name]
		if !isImport {
			return true
		}
		name := sel.Sel.Name
		switch {
		case ip == "os" && name == "Exit" && owner != "cmd":
			vs = append(vs, violation{path + ": os.Exit", exits})
		case !test && owner != "root" && world[ip] != nil && world[ip](name) && (owner != "cmd" || ip+"."+name != "os.Args"):
			vs = append(vs, violation{path + ": " + ip + "." + name, ambient})
		case ip == mod+"/internal/events" && slices.Contains(guarded, name) && !slices.Contains(readers, owner):
			vs = append(vs, violation{path + ": events." + name, opened})
		}
		return true
	})
	return vs
}

// gated is whether f's //go:build line holds it out of every build without
// the integration tag: integration is the line, or one of its conjuncts.
func gated(f *ast.File) bool {
	for _, group := range f.Comments {
		for _, c := range group.List {
			if c.Pos() < f.Package && constraint.IsGoBuild(c.Text) {
				x, err := constraint.Parse(c.Text)
				return err == nil && requires(x, "integration")
			}
		}
	}
	return false
}

// requires is whether x is false whenever tag is unset: x is tag, or a
// conjunction with tag among its terms.
func requires(x constraint.Expr, tag string) bool {
	switch x := x.(type) {
	case *constraint.TagExpr:
		return x.Tag == tag
	case *constraint.AndExpr:
		return requires(x.X, tag) || requires(x.Y, tag)
	}
	return false
}

// reach is each package of the standard library with which of its names
// reach outside the process: the os functions that make, change or
// remove a file, open a directory as an os.Root, whose methods do, or
// start a process; the os/exec commands; every syscall but its constants
// and the errno and signal types. A test's own TempDir is reach too. A
// read is not: what is on disk for a test to read is the source it was
// built from, or what one of these made. It is the one list of reach; the
// module's own is derived from it `[LAW:one-source-of-truth]`.
var reach = map[string]func(name string) bool{
	"os": oneOf("WriteFile", "Create", "CreateTemp", "OpenFile", "Mkdir", "MkdirAll", "MkdirTemp", "Remove", "RemoveAll",
		"Rename", "Symlink", "Link", "Chmod", "Chown", "Lchown", "Chtimes", "Truncate", "CopyFS", "OpenRoot", "OpenInRoot",
		"StartProcess", "FindProcess"),
	"os/exec":   oneOf("Command", "CommandContext"),
	"io/ioutil": oneOf("WriteFile", "TempDir", "TempFile"),
	"syscall": func(name string) bool {
		return strings.ToUpper(name) != name && name != "Errno" && name != "Signal"
	},
}

// ref is a top-level name of a package: a func, var or type by its name,
// a method as its type's name, a dot and its own. A test file's names are
// in their own package, its directory's path and the package's name.
type ref struct{ pkg, name string }

// decl is one declaration of a name, with the file it is in. A name has
// one in each file that declares it: a build constraint can give one name
// a declaration per platform.
type decl struct {
	s *source
	// body is what the name runs or holds: a func, a var's values; nil
	// for a type
	body ast.Node
	// fn is the func's type, nil for a var or type
	fn *ast.FuncType
}

// module is every top-level name of the tree, and which reach outside
// the process.
type module struct {
	fset  *token.FileSet
	srcs  []*source
	mod   string
	kits  map[string]bool
	decls map[ref][]decl
	// methods is each type's methods
	methods map[ref][]ref
	reaches map[ref]bool
}

func newModule(fset *token.FileSet, srcs []*source, mod string, kits map[string]bool) *module {
	m := &module{fset: fset, srcs: srcs, mod: mod, kits: kits, decls: map[ref][]decl{}, methods: map[ref][]ref{}, reaches: map[ref]bool{}}
	for _, s := range srcs {
		for _, d := range s.f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				r := ref{home(s), d.Name.Name}
				if d.Recv != nil {
					t := ref{home(s), receiver(d.Recv.List[0].Type)}
					r.name = t.name + "." + r.name
					m.methods[t] = append(m.methods[t], r)
				}
				m.decls[r] = append(m.decls[r], decl{s: s, body: d, fn: d.Type})
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						r := ref{home(s), spec.Name.Name}
						m.decls[r] = append(m.decls[r], decl{s: s})
					case *ast.ValueSpec:
						for _, n := range spec.Names {
							r := ref{home(s), n.Name}
							m.decls[r] = append(m.decls[r], decl{s: s, body: spec})
						}
					}
				}
			}
		}
	}
	m.derive()
	return m
}

// home is the package s declares its names in.
func home(s *source) string {
	if s.test {
		return s.pkg + " " + s.f.Name.Name
	}
	return s.pkg
}

// receiver is the name of the type a method's receiver is of.
func receiver(x ast.Expr) string {
	switch x := x.(type) {
	case *ast.StarExpr:
		return receiver(x.X)
	case *ast.IndexExpr:
		return receiver(x.X)
	case *ast.IndexListExpr:
		return receiver(x.X)
	case *ast.ParenExpr:
		return receiver(x.X)
	case *ast.Ident:
		return x.Name
	}
	return ""
}

// derive is the fixpoint of reach over every name: a func or var reaches
// when what it runs or holds uses a reach of the standard library, or a
// name that reaches; a type reaches when one of its methods does.
func (m *module) derive() {
	deps := map[ref][]ref{}
	for r, ds := range m.decls {
		deps[r] = m.methods[r]
		for _, d := range ds {
			if d.body == nil {
				continue
			}
			m.uses(d.s, d.body, func(u use) bool {
				m.reaches[r] = m.reaches[r] || u.std
				deps[r] = append(deps[r], u.refs...)
				return true
			})
		}
	}
	for grew := true; grew; {
		grew = false
		for r, on := range deps {
			if !m.reaches[r] && slices.ContainsFunc(on, func(o ref) bool { return m.reaches[o] }) {
				m.reaches[r], grew = true, true
			}
		}
	}
}

// use is one use of a name: a reach of the standard library, or the names
// of the module it brings in: a func or var used, the type a composite
// literal or a conversion makes, the result types of a func called.
type use struct {
	at   ast.Expr
	std  bool
	refs []ref
	// call is the call the name is the callee of, nil for any other use
	call *ast.CallExpr
}

// uses calls see on each use in n, a node of s, and walks into a call's
// arguments only when see says to `[LAW:dataflow-not-control-flow]`.
func (m *module) uses(s *source, n ast.Node, see func(use) bool) {
	locals := declared(n)
	var walk func(ast.Node) bool
	walk = func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.FuncDecl:
			ast.Inspect(n.Type, walk)
			ast.Inspect(n.Body, walk)
			return false
		case *ast.CompositeLit:
			if r, ok := m.resolve(s, made(n.Type), locals); ok {
				see(r)
			}
			for _, e := range n.Elts {
				if kv, ok := e.(*ast.KeyValueExpr); ok {
					if _, field := kv.Key.(*ast.Ident); field {
						ast.Inspect(kv.Value, walk)
						continue
					}
				}
				ast.Inspect(e, walk)
			}
			return false
		case *ast.CallExpr:
			r, ok := m.resolve(s, made(n.Fun), locals)
			if !ok {
				ast.Inspect(n.Fun, walk)
			}
			r.call = n
			if ok && !r.std {
				r.refs = append(r.refs, m.results(r.refs[0])...)
			}
			if !ok || see(r) {
				for _, a := range n.Args {
					ast.Inspect(a, walk)
				}
			}
			return false
		case *ast.SelectorExpr, *ast.Ident:
			// a type named anywhere but where a value of it is made is no use
			if r, ok := m.resolve(s, n.(ast.Expr), locals); ok {
				if r.std || !m.isType(r.refs[0]) {
					see(r)
				}
				return false
			}
			// a name of an import that reaches nothing; else a field or a
			// method of a value, of which a test's TempDir reaches
			sel, ok := n.(*ast.SelectorExpr)
			if !ok || qualified(s, sel, locals) {
				return false
			}
			if sel.Sel.Name == "TempDir" {
				see(use{at: sel, std: true})
			}
			ast.Inspect(sel.X, walk)
			return false
		}
		return true
	}
	ast.Inspect(n, walk)
}

// qualified is whether sel names a name of a package s imports.
func qualified(s *source, sel *ast.SelectorExpr, locals map[string]bool) bool {
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	_, imported := s.imported[x.Name]
	return imported && !locals[x.Name]
}

// declared is every name n declares inside it: its parameters, results
// and receivers, its fields, and the vars := , var and range make. A use
// of one is taken for the local's wherever in n it is, though a block
// can shadow less.
func declared(n ast.Node) map[string]bool {
	names := map[string]bool{}
	add := func(xs ...ast.Expr) {
		for _, x := range xs {
			if id, ok := x.(*ast.Ident); ok {
				names[id.Name] = true
			}
		}
	}
	ast.Inspect(n, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Field:
			for _, id := range n.Names {
				names[id.Name] = true
			}
		case *ast.ValueSpec:
			for _, id := range n.Names {
				names[id.Name] = true
			}
		case *ast.AssignStmt:
			if n.Tok == token.DEFINE {
				add(n.Lhs...)
			}
		case *ast.RangeStmt:
			if n.Tok == token.DEFINE {
				add(n.Key, n.Value)
			}
		}
		return true
	})
	return names
}

// made is the name of the type or func x makes or calls, through the
// parentheses, pointers, slices and instantiations around it.
func made(x ast.Expr) ast.Expr {
	switch y := x.(type) {
	case *ast.ParenExpr:
		return made(y.X)
	case *ast.StarExpr:
		return made(y.X)
	case *ast.ArrayType:
		return made(y.Elt)
	case *ast.IndexExpr:
		return made(y.X)
	case *ast.IndexListExpr:
		return made(y.X)
	}
	return x
}

func (m *module) isType(r ref) bool { return m.decls[r][0].body == nil }

// test is whether r is declared in a test file.
func (m *module) test(r ref) bool { return m.decls[r][0].s.test }

// results is the types the func r is declared to return.
func (m *module) results(r ref) []ref {
	var out []ref
	for _, d := range m.decls[r] {
		if d.fn == nil || d.fn.Results == nil {
			continue
		}
		for _, f := range d.fn.Results.List {
			if t, ok := m.resolve(d.s, made(f.Type), nil); ok && !t.std && m.isType(t.refs[0]) {
				out = append(out, t.refs...)
			}
		}
	}
	return out
}

// resolve is the name x is, by the names s's package declares and those
// s imports, unless it is one of locals: a reach of the standard library,
// or a name of the module or of s's test package. The tree is read
// without types, so the go command, which they need, never runs
// `[LAW:effects-at-boundaries]`.
func (m *module) resolve(s *source, x ast.Expr, locals map[string]bool) (use, bool) {
	switch x := x.(type) {
	case *ast.SelectorExpr:
		if !qualified(s, x, locals) {
			return use{}, false
		}
		return m.named(x, s.imported[x.X.(*ast.Ident).Name], x.Sel.Name)
	case *ast.Ident:
		if locals[x.Name] {
			return use{}, false
		}
		homes := []string{home(s)}
		if s.test && !strings.HasSuffix(s.f.Name.Name, "_test") {
			homes = append(homes, s.pkg)
		}
		for _, h := range homes {
			if _, ok := m.decls[ref{h, x.Name}]; ok {
				return use{at: x, refs: []ref{{h, x.Name}}}, true
			}
		}
		for _, imp := range s.f.Imports {
			ip := strings.Trim(imp.Path.Value, "`\"")
			if imp.Name == nil || imp.Name.Name != "." {
				continue
			}
			if u, ok := m.named(x, ip, x.Name); ok {
				return u, true
			}
		}
	}
	return use{}, false
}

// named is the name name of the package at ip, as x uses it.
func (m *module) named(x ast.Expr, ip, name string) (use, bool) {
	if called, ok := reach[ip]; ok && called(name) {
		return use{at: x, std: true}, true
	}
	_, ok := m.decls[ref{ip, name}]
	return use{at: x, refs: []ref{{ip, name}}}, ok && strings.HasPrefix(ip, m.mod+"/")
}

// reaching is every use in a test without the integration build
// constraint of a reach or of a name that reaches, walked from what the
// test binary runs, its tests, benchmarks, fuzz targets, examples,
// TestMain, init and package vars, through the test package's own funcs
// and the methods of the types it makes. A port's contract suite is not
// walked into, nor are its arguments: the suite holds its fake and its
// real adapter to one behavior side by side, so the fast loop keeps the
// fake honest (D2). It is the fast loop's one gate `[LAW:single-enforcer]`.
func (m *module) reaching() []violation {
	var vs []violation
	walked := map[ref]bool{}
	var walk func(r ref)
	see := func(u use) bool {
		if u.call != nil && !u.std && m.suite(u.refs[0]) {
			return false
		}
		for _, r := range u.refs {
			if m.test(r) {
				walk(r)
			}
		}
		if u.std || slices.ContainsFunc(u.refs, func(r ref) bool { return !m.test(r) && m.reaches[r] }) {
			vs = append(vs, violation{m.fset.Position(u.at.Pos()).String() + ": " + types.ExprString(u.at), reaches})
		}
		return true
	}
	walk = func(r ref) {
		if walked[r] {
			return
		}
		walked[r] = true
		for _, method := range m.methods[r] {
			walk(method)
		}
		// a declaration in a file the fast loop does not build is not the one it runs
		for _, d := range m.decls[r] {
			if d.body != nil && !d.s.gated {
				m.uses(d.s, d.body, see)
			}
		}
	}
	for _, s := range m.srcs {
		if !s.test || s.gated {
			continue
		}
		for _, d := range s.f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && run(d.Name.Name) {
					walk(ref{home(s), d.Name.Name})
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					if v, ok := spec.(*ast.ValueSpec); ok {
						for _, n := range v.Names {
							walk(ref{home(s), n.Name})
						}
					}
				}
			}
		}
	}
	return vs
}

// run is whether a test binary runs the func named name itself.
func run(name string) bool {
	for _, prefix := range []string{"Test", "Benchmark", "Fuzz", "Example"} {
		if rest, ok := strings.CutPrefix(name, prefix); ok && (rest == "" || !unicode.IsLower(rune(rest[0]))) {
			return true
		}
	}
	return name == "init"
}

// suite is whether r is a port's contract suite: a func of a test kit or
// of a test package that takes a test and the factory of the port, a func
// of a test to one value.
func (m *module) suite(r ref) bool {
	return (m.test(r) || m.kits[r.pkg]) && slices.ContainsFunc(m.decls[r], func(d decl) bool {
		if d.fn == nil {
			return false
		}
		var test, factory bool
		for _, p := range d.fn.Params.List {
			test = test || aTest(d.s, p.Type)
			fn, ok := p.Type.(*ast.FuncType)
			factory = factory || ok && fields(fn.Params) == 1 && aTest(d.s, fn.Params.List[0].Type) && fields(fn.Results) == 1
		}
		return test && factory
	})
}

// aTest is whether x, in s, is *testing.T or testing.TB.
func aTest(s *source, x ast.Expr) bool {
	star, isStar := x.(*ast.StarExpr)
	if isStar {
		x = star.X
	}
	sel, ok := x.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && s.imported[pkg.Name] == "testing" && (isStar && sel.Sel.Name == "T" || !isStar && sel.Sel.Name == "TB")
}

// fields is how many values fl declares.
func fields(fl *ast.FieldList) int {
	if fl == nil {
		return 0
	}
	n := 0
	for _, f := range fl.List {
		n += max(1, len(f.Names))
	}
	return n
}

// localName is the name a file refers to an import by: its own name, else
// the last path element that is not a major version.
func localName(imp *ast.ImportSpec, ip string) string {
	if imp.Name != nil {
		return imp.Name.Name
	}
	elems := strings.Split(ip, "/")
	if n := len(elems); n > 1 && majorVersion.MatchString(elems[n-1]) {
		return elems[n-2]
	}
	return elems[len(elems)-1]
}

func declaredFor(declared map[string]string, ip, owner string) bool {
	for module, unit := range declared {
		if (ip == module || strings.HasPrefix(ip, module+"/")) && unit == owner {
			return true
		}
	}
	return false
}

// world is each package that can read the process's world, with which of
// its names do: its clock and timers, its environment, host, process, user
// and directories, its arguments and streams, its entropy, the standard
// logger, or the shared source behind math/rand's top-level functions. A
// dot import of one hides its names from the selector check, so the import
// is itself a read.
var world = map[string]func(name string) bool{
	"time": oneOf("Now", "Since", "Until", "After", "Tick", "Sleep", "NewTimer", "NewTicker", "AfterFunc", "Local"),
	"os": oneOf("Getenv", "LookupEnv", "Environ", "ExpandEnv", "Getwd", "Args", "Stdin", "Stdout", "Stderr",
		"Hostname", "Getpid", "Getppid", "Getuid", "Geteuid", "Getgid", "Getegid",
		"UserHomeDir", "UserConfigDir", "UserCacheDir", "TempDir", "Executable"),
	"crypto/rand":  oneOf("Reader", "Read", "Text"),
	"fmt":          oneOf("Print", "Printf", "Println"),
	"log":          everyName, // the standard logger writes stderr, and log.Fatal* calls os.Exit
	"os/user":      everyName,
	"math/rand":    sharedSource,
	"math/rand/v2": sharedSource,
}

func oneOf(names ...string) func(string) bool {
	return func(name string) bool { return slices.Contains(names, name) }
}

func everyName(string) bool { return true }

// sharedSource is every math/rand name but the constructors and types,
// which draw from a source the caller gives.
func sharedSource(name string) bool {
	return !slices.Contains([]string{"New", "NewSource", "NewPCG", "NewChaCha8", "NewZipf", "Rand", "Source", "Source64", "PCG", "ChaCha8", "Zipf"}, name)
}

var (
	majorVersion = regexp.MustCompile(`^v[0-9]+$`)
	heading      = regexp.MustCompile(`^### (3\.[0-9]+) (.+)$`)
	step         = regexp.MustCompile(`^([0-9]+)\. \*\*([^*]+?)\.?\*\*`)
	unitRef      = regexp.MustCompile("`([^`]+)`")
)

// checkConcerns holds the concern table to architecture.md: a row for every
// §3 primitive and every §5 step, and every row naming listed units.
func checkConcerns(note, design string, rank map[string]int) []violation {
	var vs []violation
	rows := map[string]bool{}
	for line := range strings.Lines(note) {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		concern := strings.TrimSpace(cells[0])
		if len(cells) < 2 || concern == "concern" || strings.HasPrefix(concern, "---") {
			continue
		}
		rows[concern] = true
		units := unitRef.FindAllStringSubmatch(cells[1], -1)
		named := len(units) > 0
		for _, u := range units {
			_, listed := rank[u[1]]
			named = named && listed
		}
		if !named {
			vs = append(vs, violation{concern, rowUnit})
		}
	}
	var section string
	for line := range strings.Lines(design) {
		line = strings.TrimRight(line, "\n")
		if strings.HasPrefix(line, "## ") {
			section = line
		}
		var concern string
		if m := heading.FindStringSubmatch(line); m != nil {
			concern = "§" + m[1] + " " + m[2]
		}
		if m := step.FindStringSubmatch(line); m != nil && strings.HasPrefix(section, "## 5.") {
			concern = "§5." + m[1] + " " + m[2]
		}
		if concern != "" && !rows[concern] {
			vs = append(vs, violation{concern, missingRow})
		}
	}
	return vs
}

// checkDrawing holds §6's drawing to the layers block: its rows, the
// least-indented lines of the section's first fenced block, name the units
// from cli to git in order.
func checkDrawing(design string, layers []string) []violation {
	var rows []string
	var section string
	indent, fences := -1, 0
	for line := range strings.Lines(design) {
		line = strings.TrimRight(line, "\n")
		if strings.HasPrefix(line, "## ") {
			section = line
		}
		if !strings.HasPrefix(section, "## 6.") || fences > 1 {
			continue
		}
		if strings.HasPrefix(line, "```") {
			fences++
			continue
		}
		depth := len(line) - len(strings.TrimLeft(line, " "))
		if fences != 1 || strings.TrimSpace(line) == "" || indent >= 0 && depth > indent {
			continue
		}
		indent = depth
		rows = append(rows, strings.Fields(line)[0])
	}
	from, to := slices.Index(layers, "cli"), slices.Index(layers, "git")
	if from < 0 || to < from || !slices.Equal(rows, layers[from:to+1]) {
		return []violation{{fmt.Sprintf("§6's drawing %v", rows), drawing}}
	}
	return nil
}

func TestTheTreeKeepsItsArchitecture(t *testing.T) {
	vs, err := check(os.DirFS("../.."))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		t.Error(v)
	}
}

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

const (
	note = "```layers\nroot\ncli\ngit\nstore\nfiles\nreason\n```\n\n```dependencies\n```\n\n" +
		"| concern | unit |\n|---|---|\n| §3.1 Node and path | `cli` |\n| §5.1 Open | `git` |\n"
	design = "## 3. The primitives\n\n### 3.1 Node and path\n\n## 5. One command\n\n1. **Open.** Read the heads.\n\n" +
		"## 6. The parts, downhill\n\n```\n cli        argv\n git        objects and refs;\n            for push and fetch\n```\n"
)

// synthetic is a small module that breaks nothing: a program whose root
// opens a store on disk and runs git, each port with an adapter and a
// fake, and gittest and filestest, the test kits.
func synthetic() fstest.MapFS {
	return fstest.MapFS{
		"go.mod":                      file("module example.com/m\n\ngo 1.26\n"),
		"ARCHITECTURE.md":             file(note),
		"design-docs/architecture.md": file(design),
		"cmd/lit2/main.go":            file("package main\n\nimport (\n\t\"os\"\n\n\t\"example.com/m/internal/root\"\n)\n\nfunc main() { os.Exit(root.Main(os.Args[1:])) }\n"),
		"internal/root/root.go": file(`package root

import (
	"time"

	"example.com/m/internal/cli"
	"example.com/m/internal/git"
	"example.com/m/internal/store"
)

func Main(args []string) int {
	_ = store.DiskPorts("/")
	_ = git.Find(nil).Run("status")
	return cli.Run(time.Now(), args)
}
`),
		"internal/cli/cli.go":       file("package cli\n\nimport \"example.com/m/internal/reason\"\n\nvar Run = reason.Run\n"),
		"internal/reason/reason.go": file("package reason\n"),
		"internal/git/git.go": file(`package git

import "os/exec"

type Git struct{ env []string }

func Find(env []string) *Git { return &Git{env: env} }

func (g *Git) Run(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Env = g.env
	return cmd.Run()
}

type Objects interface{ Put(b []byte) error }

type repository struct {
	dir string
	g   *Git
}

func At(dir string, g *Git) Objects { return repository{dir: dir, g: g} }

func (r repository) Put([]byte) error { return r.g.Run("--git-dir="+r.dir, "hash-object", "-w", "--stdin") }

type Memory struct{}

func (Memory) Put([]byte) error { return nil }
`),
		"internal/git/gittest/gittest.go": file(`package gittest

import (
	"os/exec"
	"path/filepath"
	"testing"

	"example.com/m/internal/git"
)

func Env(t testing.TB) []string {
	bin, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	return []string{"PATH=" + filepath.Dir(bin)}
}

func Runner(t testing.TB) *git.Git { return git.Find(Env(t)) }

func Clone(t testing.TB) string {
	dir := t.TempDir()
	if err := Runner(t).Run("init", dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func Objects(t *testing.T, fresh func(*testing.T) git.Objects) {
	if err := fresh(t).Put(nil); err != nil {
		t.Fatal(err)
	}
}
`),
		"internal/files/files.go": file(`package files

import "os"

type Files interface{ CreateOnce(b []byte) error }

type Dir struct{ base string }

func DirAt(base string) Dir { return Dir{base: base} }

type Disk struct{ dir Dir }

func At(d Dir) Disk { return Disk{dir: d} }

func (o Disk) CreateOnce(b []byte) error { return os.WriteFile(o.dir.base, b, 0o644) }

type Memory struct{ held []byte }

func (m *Memory) CreateOnce(b []byte) error {
	m.held = b
	return nil
}
`),
		"internal/files/filestest/filestest.go": file(`package filestest

import (
	"testing"

	"example.com/m/internal/files"
)

func Files(t *testing.T, fresh func(*testing.T) files.Files) {
	if err := fresh(t).CreateOnce(nil); err != nil {
		t.Fatal(err)
	}
}

func Memory() *files.Memory { return &files.Memory{} }

type Seams struct{ fail func(path string) error }

func Recording(seen *[]string, fail func(path string) error) Seams { return Seams{fail: fail} }
`),
		"internal/store/store.go": file(`package store

import "example.com/m/internal/files"

func DiskPorts(base string) files.Files { return files.At(files.DirAt(base)) }

func MemoryRepositoryPorts() files.Files { return &files.Memory{} }
`),
	}
}

func TestTheCheckerNamesEachBreak(t *testing.T) {
	if vs, err := check(synthetic()); err != nil || len(vs) != 0 {
		t.Fatalf("the synthetic module breaks %v, %v", vs, err)
	}
	for _, tc := range []struct {
		name string
		path string
		text string
		want rule
	}{
		{"an upward import", "internal/reason/reason.go", "package reason\n\nimport _ \"example.com/m/internal/cli\"\n", upward},
		{"an unlisted unit", "internal/extra/extra.go", "package extra\n", unlisted},
		{"an undeclared module", "internal/git/git.go", "package git\n\nimport _ \"github.com/go-git/go-git/v5\"\n", undeclared},
		{"the program importing past root", "cmd/lit2/main.go", "package main\n\nimport _ \"example.com/m/internal/cli\"\n", program},
		{"time.Now outside root", "internal/cli/cli.go", "package cli\n\nimport \"time\"\n\nvar At = time.Now()\n", ambient},
		{"an aliased math/rand/v2 global", "internal/git/git.go", "package git\n\nimport r \"math/rand/v2\"\n\nvar N = r.IntN(9)\n", ambient},
		{"crypto/rand.Text outside root", "internal/git/git.go", "package git\n\nimport \"crypto/rand\"\n\nvar S = rand.Text()\n", ambient},
		{"a timer outside root", "internal/cli/cli.go", "package cli\n\nimport \"time\"\n\nfunc Wait() { time.Sleep(time.Second) }\n", ambient},
		{"the standard logger outside root", "internal/cli/cli.go", "package cli\n\nimport \"log\"\n\nfunc Say() { log.Printf(\"hi\") }\n", ambient},
		{"the host outside root", "internal/git/git.go", "package git\n\nimport \"os\"\n\nvar H, _ = os.Hostname()\n", ambient},
		{"a dot import of time outside root", "internal/cli/cli.go", "package cli\n\nimport . \"time\"\n\nvar At = Now()\n", ambient},
		{"os/user outside root", "internal/git/git.go", "package git\n\nimport \"os/user\"\n\nvar U, _ = user.Current()\n", ambient},
		{"the effective uid outside root", "internal/git/git.go", "package git\n\nimport \"os\"\n\nvar U = os.Geteuid()\n", ambient},
		{"the local zone outside root", "internal/cli/cli.go", "package cli\n\nimport \"time\"\n\nvar Z = time.Local\n", ambient},
		{"net, even in root", "internal/root/root.go", "package root\n\nimport _ \"net\"\n", network},
		{"a net/ package, even in a test", "internal/git/git_test.go", "package git\n\nimport _ \"net/http\"\n", network},
		{"crypto/tls in the program", "cmd/lit2/main.go", "package main\n\nimport _ \"crypto/tls\"\n", network},
		{"a file under internal/", "internal/README", "notes\n", unlisted},
		{"os.Exit outside cmd/lit2", "internal/root/exit.go", "package root\n\nimport \"os\"\n\nfunc init() { os.Exit(0) }\n", exits},
		{"go:embed outside assets", "internal/git/git.go", "package git\n\nimport _ \"embed\"\n\n//go:embed x.txt\nvar X string\n", embeds},
		{"a §3 heading with no row", "design-docs/architecture.md", strings.Replace(design, "## 5.", "### 3.2 Event and intent\n\n## 5.", 1), missingRow},
		{"a §5 step with no row", "design-docs/architecture.md", strings.Replace(design, "## 6.", "2. **Parse** the file.\n\n## 6.", 1), missingRow},
		{"a row naming an unlisted unit", "ARCHITECTURE.md", strings.Replace(note, "`git` |", "`gitt` |", 1), rowUnit},
		{"a §6 drawing out of the layers order", "design-docs/architecture.md", strings.Replace(design, " cli        argv\n git", " git        argv\n cli", 1), drawing},
		{"no layers block", "ARCHITECTURE.md", strings.Replace(note, "```layers", "```", 1), noBlock},
		{"an envelope opened in fold", "internal/fold/fold.go", "package fold\n\nimport \"example.com/m/internal/events\"\n\nvar Read = events.Open\n", opened},
		{"a keyring built in fold's test", "internal/fold/fold_test.go", "package fold\n\nimport \"example.com/m/internal/events\"\n\nvar K = events.NewKeyring()\n", opened},
		{"a keyring literal in fold", "internal/fold/fold.go", "package fold\n\nimport \"example.com/m/internal/events\"\n\nvar K = events.Keyring{}\n", opened},
		{"events dot-imported in fold", "internal/fold/fold.go", "package fold\n\nimport . \"example.com/m/internal/events\"\n\nvar Read = Open\n", opened},
		{"a temporary directory in a fast test", "internal/git/git_test.go", "package git\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { _ = t.TempDir() }\n", reaches},
		{"a file written by a fast test", "internal/git/git_test.go", "package git\n\nimport \"os\"\n\nvar Err = os.WriteFile(\"x\", nil, 0o644)\n", reaches},
		{"a process in a fast test", "internal/git/git_test.go", "package git\n\nimport \"os/exec\"\n\nvar Err = exec.Command(\"git\").Run()\n", reaches},
		{"a syscall in a fast test", "internal/git/git_test.go", "package git\n\nimport \"syscall\"\n\nvar Err = syscall.Mkfifo(\"x\", 0o644)\n", reaches},
		{"git in a fast test", "internal/cli/cli_test.go", "package cli\n\nimport (\n\t\"testing\"\n\n\t\"example.com/m/internal/git/gittest\"\n)\n\nfunc TestX(t *testing.T) { gittest.Clone(t) }\n", reaches},
		{"the store on disk in a fast test", "internal/cli/cli_test.go", "package cli\n\nimport \"example.com/m/internal/store\"\n\nvar P = store.DiskPorts(\"/\")\n", reaches},
		{"a disk factory given to no contract suite", "internal/git/git_test.go", factory("run("), reaches},
		{"a gate that is no conjunct of integration", "internal/git/git_test.go", "//go:build !integration || unix\n\n" + factory("run("), reaches},
		{"a disk port's create in a fast test", "internal/files/files_test.go", fast("files_test", "\"example.com/m/internal/files\"", "_ = files.At(files.DirAt(\"/var/tmp\")).CreateOnce(nil)"), reaches},
		{"the program's root in a fast test", "internal/root/root_test.go", fast("root_test", "\"example.com/m/internal/root\"", "root.Main(nil)"), reaches},
		{"git found by a fast test of git's own package", "internal/git/git_test.go", fast("git", "", "Find(nil)"), reaches},
		{"a write by a dot import of os", "internal/git/git_test.go", fast("git", ". \"os\"", "_ = WriteFile(\"x\", nil, 0o644)"), reaches},
		{"a write held in a var", "internal/git/git_test.go", fast("git", "\"os\"", "w := os.WriteFile\n\t_ = w(\"x\", nil, 0o644)"), reaches},
		{"a directory opened as an os.Root", "internal/git/git_test.go", fast("git", "\"os\"", "r, _ := os.OpenRoot(\"/tmp\")\n\t_ = r.Mkdir(\"x\", 0o755)"), reaches},
		{"a write given to a test kit's func that is no contract suite", "internal/files/files_test.go",
			fast("files_test", "\"os\"\n\n\t\"example.com/m/internal/files/filestest\"", "var seen []string\n\t_ = filestest.Recording(&seen, func(p string) error { return os.WriteFile(p, nil, 0o644) })"), reaches},
		{"a helper of a fast test that reaches", "internal/git/git_test.go", fast("git", "", "helper(t)") + "\nfunc helper(t *testing.T) { _ = t.TempDir() }\n", reaches},
		{"a disk port made by a fast test's var", "internal/store/store_test.go", "package store\n\nvar P = DiskPorts(\"/\")\n", reaches},
		{"the program's test importing a unit that is no test kit", "cmd/lit2/smoke_test.go", "//go:build integration\n\npackage main\n\nimport _ \"example.com/m/internal/store\"\n", program},
	} {
		tree := synthetic()
		tree[tc.path] = file(tc.text)
		vs, err := check(tree)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !slices.ContainsFunc(vs, func(v violation) bool { return v.rule == tc.want }) {
			t.Errorf("%s: the checker names %v; want a violation that %s", tc.name, vs, tc.want)
		}
	}
}

// A rename in events would void the rule without a word, so the rule's
// names are checked against the package they guard.
func TestEventsDeclaresEveryNameTheEnvelopeRuleGuards(t *testing.T) {
	declared := map[string]bool{}
	fset := token.NewFileSet()
	files, err := filepath.Glob("../events/*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil {
					declared[d.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, s := range d.Specs {
					if ts, ok := s.(*ast.TypeSpec); ok {
						declared[ts.Name.Name] = true
					}
				}
			}
		}
	}
	for _, name := range guarded {
		if !declared[name] {
			t.Errorf("internal/events declares no %s, which the envelope rule guards", name)
		}
	}
}

func TestTheReadersOpenEnvelopes(t *testing.T) {
	tree := synthetic()
	tree["internal/cli/render.go"] = file("package cli\n\nimport \"example.com/m/internal/events\"\n\nvar Read = events.Open\n")
	tree["internal/root/keys.go"] = file("package root\n\nimport \"example.com/m/internal/events\"\n\nvar K = events.NewKeyring()\n")
	if vs, err := check(tree); err != nil || len(vs) != 0 {
		t.Errorf("cli opening an envelope and root building the keyring break %v, %v; want nothing", vs, err)
	}
}

// fast is an untagged test file of package pkg importing imports, whose
// one test runs body.
func fast(pkg, imports, body string) string {
	return "package " + pkg + "\n\nimport (\n\t\"testing\"\n\t" + imports + "\n)\n\nfunc TestX(t *testing.T) {\n\t" + body + "\n}\n"
}

// factory is a test file whose test gives a factory reaching the disk and
// git to suite; run takes a factory and no test, so it is no contract
// suite.
func factory(suite string) string {
	return "package git_test\n\nimport (\n\t\"testing\"\n\n\t\"example.com/m/internal/git\"\n\t\"example.com/m/internal/git/gittest\"\n)\n\n" +
		"func run(fresh func(*testing.T) git.Objects) {}\n\n" +
		"func TestObjects(t *testing.T) {\n\t" + suite + "func(t *testing.T) git.Objects { return git.At(t.TempDir(), gittest.Runner(t)) })\n}\n"
}

// A test reaches the disk, git or a process unseen by the rule only in the
// integration build, or in a contract suite and the factories it is given,
// a func literal or a named func, the suite a test kit's or the test's
// own; a test kit's read reaches nothing, nor does a fake; the program's
// test imports a test kit.
func TestTheIntegrationGateAndAContractsFactoryReachTheDisk(t *testing.T) {
	tree := synthetic()
	tree["internal/git/objects_test.go"] = file(factory("gittest.Objects(t, "))
	tree["internal/git/named_test.go"] = file(`package git_test

import (
	"testing"

	"example.com/m/internal/git"
	"example.com/m/internal/git/gittest"
)

func onDisk(t *testing.T) git.Objects { return git.At(gittest.Clone(t), gittest.Runner(t)) }

func TestNamed(t *testing.T) { gittest.Objects(t, onDisk) }

func contract(t *testing.T, fresh func(*testing.T) git.Objects) {
	if err := fresh(t).Put(nil); err != nil {
		t.Fatal(err)
	}
}

func TestOwn(t *testing.T) { contract(t, onDisk) }

func TestOwnInMemory(t *testing.T) { contract(t, func(*testing.T) git.Objects { return git.Memory{} }) }

func TestEnv(t *testing.T) { _ = gittest.Env(t) }
`)
	tree["internal/root/fake_test.go"] = file(fast("root_test", "\"os\"\n\n\t\"example.com/m/internal/files/filestest\"\n\t\"example.com/m/internal/store\"",
		"_ = store.MemoryRepositoryPorts().CreateOnce(nil)\n\t_ = filestest.Memory().CreateOnce(nil)\n\t_ = os.TempDir()"))
	tree["internal/git/run_test.go"] = file("//go:build integration\n\n" + fast("git", "", "_ = Find(nil).Run(\"status\")\n\t_ = t.TempDir()"))
	tree["internal/git/fifo_test.go"] = file("//go:build unix && integration\n\npackage git\n\nimport \"syscall\"\n\nvar Err = syscall.Mkfifo(\"x\", 0o644)\n")
	tree["cmd/lit2/smoke_test.go"] = file("//go:build integration\n\npackage main\n\nimport (\n\t\"os/exec\"\n\n\t\"example.com/m/internal/git/gittest\"\n)\n\nvar Env, Cmd = gittest.Env, exec.Command\n")
	if vs, err := check(tree); err != nil || len(vs) != 0 {
		t.Errorf("contract suites' disk factories, a kit's read, the fakes, gated tests' reach and the program's test importing gittest break %v, %v; want nothing", vs, err)
	}
}

// A helper a test calls from another test file of its package is walked
// as one in its own.
func TestAHelperInAnotherTestFileReaches(t *testing.T) {
	tree := synthetic()
	tree["internal/git/git_test.go"] = file(fast("git", "", "helper(t)"))
	tree["internal/git/helper_test.go"] = file("package git\n\nimport \"testing\"\n\nfunc helper(t *testing.T) { _ = t.TempDir() }\n")
	vs, err := check(tree)
	if err != nil || len(vs) != 1 || vs[0].rule != reaches || !strings.HasPrefix(vs[0].where, "internal/git/helper_test.go:") {
		t.Errorf("the checker names %v, %v; want the helper's TempDir, in its own file", vs, err)
	}
}

func TestTheCheckerPassesByWhatTheGoToolIgnores(t *testing.T) {
	tree := synthetic()
	tree["internal/.DS_Store"] = file("\x00\x00\x00\x01Bud1")
	tree["internal/_scratch/scratch.go"] = file("not go")
	tree["internal/cli/testdata/bad.go"] = file("package bad\n\nfunc {\n")
	if vs, err := check(tree); err != nil || len(vs) != 0 {
		t.Errorf("the checker names %v, %v; want nothing for what the go tool ignores", vs, err)
	}
}
