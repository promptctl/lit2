// Package architecture holds every import in the tree to ARCHITECTURE.md:
// its layers block is the one source of the dependency order and this test
// is its one enforcer `[LAW:one-way-deps]` `[LAW:single-enforcer]` (D5).
package architecture

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// rule is what a violation breaks; a test finds a violation by its rule.
type rule string

const (
	upward     rule = "imports a unit not below it in the layers block"
	unlisted   rule = "is under internal/ and is not a unit in the layers block"
	undeclared rule = "imports a module the dependencies block does not give its unit"
	program    rule = "is the program and imports a unit other than root"
	ambient    rule = "reads the process's world outside root"
	exits      rule = "calls os.Exit outside cmd/lit2"
	missingRow rule = "has no row in ARCHITECTURE.md's concern table"
	rowUnit    rule = "has a concern row naming no unit, or one not in the layers block"
	drawing    rule = "is not the layers block's order from cli to git"
	noBlock    rule = "is missing from ARCHITECTURE.md"
)

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
			f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution)
			if err != nil {
				return err
			}
			owner := strings.Split(path, "/")[1]
			if top == "cmd" {
				owner = "cmd"
			}
			vs = append(vs, checkFile(path, owner, f, mod, rank, declared)...)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return vs, nil
}

// ignored is whether the go tool passes a file or directory by, as it does
// testdata and any name starting with . or _, so it is no part of the tree.
func ignored(name string) bool {
	return name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

func checkFile(path, owner string, f *ast.File, mod string, rank map[string]int, declared map[string]string) []violation {
	var vs []violation
	test := strings.HasSuffix(path, "_test.go")
	imported := map[string]string{} // the name a file uses for an import → its path
	for _, imp := range f.Imports {
		ip := strings.Trim(imp.Path.Value, "`\"")
		imported[localName(imp, ip)] = ip
		if imp.Name != nil && imp.Name.Name == "." && world[ip] != nil && !test && owner != "root" {
			vs = append(vs, violation{path + ": import . " + ip, ambient})
		}
		at := violation{path + ": import " + ip, ""}
		if rest, ok := strings.CutPrefix(ip, mod+"/internal/"); ok {
			unit := strings.Split(rest, "/")[0]
			to, listed := rank[unit]
			from, owned := rank[owner]
			switch {
			case owner == "cmd" && unit != "root":
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
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		x, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		ip, isImport := imported[x.Name]
		if !isImport {
			return true
		}
		name := sel.Sel.Name
		switch {
		case ip == "os" && name == "Exit" && owner != "cmd":
			vs = append(vs, violation{path + ": os.Exit", exits})
		case !test && owner != "root" && world[ip] != nil && world[ip](name) && (owner != "cmd" || ip+"."+name != "os.Args"):
			vs = append(vs, violation{path + ": " + ip + "." + name, ambient})
		}
		return true
	})
	return vs
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
	"time": oneOf("Now", "Since", "Until", "After", "Tick", "Sleep", "NewTimer", "NewTicker", "AfterFunc"),
	"os": oneOf("Getenv", "LookupEnv", "Environ", "ExpandEnv", "Getwd", "Args", "Stdin", "Stdout", "Stderr",
		"Hostname", "Getpid", "Getuid", "UserHomeDir", "UserConfigDir", "UserCacheDir", "TempDir", "Executable"),
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
	note = "```layers\nroot\ncli\ngit\nreason\n```\n\n```dependencies\n```\n\n" +
		"| concern | unit |\n|---|---|\n| §3.1 Node and path | `cli` |\n| §5.1 Open | `git` |\n"
	design = "## 3. The primitives\n\n### 3.1 Node and path\n\n## 5. One command\n\n1. **Open.** Read the heads.\n\n" +
		"## 6. The parts, downhill\n\n```\n cli        argv\n git        objects and refs;\n            for push and fetch\n```\n"
)

// synthetic is a small module that breaks nothing.
func synthetic() fstest.MapFS {
	return fstest.MapFS{
		"go.mod":                      file("module example.com/m\n\ngo 1.26\n"),
		"ARCHITECTURE.md":             file(note),
		"design-docs/architecture.md": file(design),
		"cmd/lit2/main.go":            file("package main\n\nimport (\n\t\"os\"\n\n\t\"example.com/m/internal/root\"\n)\n\nfunc main() { os.Exit(root.Main(os.Args[1:])) }\n"),
		"internal/root/root.go":       file("package root\n\nimport (\n\t\"time\"\n\n\t\"example.com/m/internal/cli\"\n)\n\nfunc Main(args []string) int { return cli.Run(time.Now(), args) }\n"),
		"internal/cli/cli.go":         file("package cli\n\nimport \"example.com/m/internal/reason\"\n\nvar Run = reason.Run\n"),
		"internal/git/git.go":         file("package git\n"),
		"internal/reason/reason.go":   file("package reason\n"),
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
		{"a file under internal/", "internal/README", "notes\n", unlisted},
		{"os.Exit outside cmd/lit2", "internal/root/exit.go", "package root\n\nimport \"os\"\n\nfunc init() { os.Exit(0) }\n", exits},
		{"a §3 heading with no row", "design-docs/architecture.md", strings.Replace(design, "## 5.", "### 3.2 Event and intent\n\n## 5.", 1), missingRow},
		{"a §5 step with no row", "design-docs/architecture.md", strings.Replace(design, "## 6.", "2. **Parse** the file.\n\n## 6.", 1), missingRow},
		{"a row naming an unlisted unit", "ARCHITECTURE.md", strings.Replace(note, "`git` |", "`gitt` |", 1), rowUnit},
		{"a §6 drawing out of the layers order", "design-docs/architecture.md", strings.Replace(design, " cli        argv\n git", " git        argv\n cli", 1), drawing},
		{"no layers block", "ARCHITECTURE.md", strings.Replace(note, "```layers", "```", 1), noBlock},
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

func TestTheCheckerPassesByWhatTheGoToolIgnores(t *testing.T) {
	tree := synthetic()
	tree["internal/.DS_Store"] = file("\x00\x00\x00\x01Bud1")
	tree["internal/_scratch/scratch.go"] = file("not go")
	tree["internal/cli/testdata/bad.go"] = file("package bad\n\nfunc {\n")
	if vs, err := check(tree); err != nil || len(vs) != 0 {
		t.Errorf("the checker names %v, %v; want nothing for what the go tool ignores", vs, err)
	}
}
