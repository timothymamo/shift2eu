package lint_test

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/timothymamo/shift2eu/test/internal/execenv"
)

// compositionRootDirs are the composition roots these source checks hold
// (issue #111): cmd/shift2eu. The HTTP API's root went with the API (ADR
// 0037).
var compositionRootDirs = []string{"cmd/shift2eu"}

// cliDir is the driving adapter a composition root hands the work to.
const cliDir = "internal/cli"

// inventoryFileDir is the source adapter the composition root constructs, so a
// root hands it the work the same way it hands it to a driving adapter: the
// group inventoryfile-no-env refuses its environment reads (issue #152), and this
// check refuses a literal that names the environment of a process.
const inventoryFileDir = "internal/adapter/source/inventoryfile"

// environmentPathDirs are the directories whose every Go file, _test.go files
// included, may hold neither a //go:linkname directive nor a literal that names
// the environment of a process: the composition root and, since issue #152,
// the driving adapter and the source adapter it hands the work to,
// internal/cli and internal/adapter/source/inventoryfile. A root that may not
// read the environment itself may hand the read to a package it constructs, so
// the two source checks that keep a linknamed symbol and a /proc path out of a
// root hold it out of those packages as well. internal/cli's own depguard rule
// refuses syscall, unsafe and os/exec, inventoryfile's refuses syscall, unsafe
// and os/exec too, and forbidigo's cli-no-env-or-process and
// inventoryfile-no-env groups refuse the environment calls; neither an import
// rule nor a call rule sees a directive or a path literal, which is why these
// checks are here.
var environmentPathDirs = slices.Concat(compositionRootDirs, []string{cliDir, inventoryFileDir})

// exprText renders expr as the source writes it, without its positions, for a
// message about an expression a check refuses.
func exprText(expr ast.Expr) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, token.NewFileSet(), expr); err != nil {
		return "an expression this check cannot render"
	}

	return buf.String()
}

// importedNames returns every local name f gives the package at path, with the
// position of each import, in source order, and nil when f imports no such
// package. A file may import one path twice, under two names: then every name
// is a way to reach the package, and a check that reads only the first one
// leaves the second unchecked. A dot import is returned as ".", a blank import
// as "_".
func importedNames(f *ast.File, path string) (names []string, positions []token.Pos) {
	for _, imp := range f.Imports {
		if imp.Path.Value != strconv.Quote(path) {
			continue
		}

		name := path
		if imp.Name != nil {
			name = imp.Name.Name
		}

		names = append(names, name)
		positions = append(positions, imp.Pos())
	}

	return names, positions
}

// anyName returns the name of the identifier expr, when expr is an identifier
// and one of names, the local names a file gives one imported package.
func anyName(expr ast.Expr, names []string) (string, bool) {
	id, ok := expr.(*ast.Ident)
	if !ok || !slices.Contains(names, id.Name) {
		return "", false
	}

	return id.Name, true
}

// environmentPathProblems returns what is wrong with the string literals of src,
// the content of the Go file at rel, one line each: a literal with a path segment
// execenv.EnvironmentPathIn matches (proc or dev/fd, the matcher this check shares
// with the e2e tier's since issue #195) names the environment of a process, which
// a composition root, and the package it hands the work to, never read, its tests
// included. It reads a literal as the compiler does, so a raw and a quoted string
// are read alike; a path assembled from parts at run time, such as
// filepath.Join("/proc", "self", "environ"), is beyond a check of literals. What
// keeps os out of the layers below the composition roots is depguard's
// domain-is-pure and app-uses-ports-not-adapters; internal/cli's own rule
// (cli-gets-adapters-from-cmd) allows os, which reads the inventory a user names,
// so this check holds it, as it holds the root and
// internal/adapter/source/inventoryfile.
func environmentPathProblems(rel string, src []byte) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, rel, src, parser.SkipObjectResolution)
	if err != nil {
		return nil, err
	}
	var problems []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			return true // a literal the parser read and the compiler refuses
		}
		if path, ok := execenv.EnvironmentPathIn(value); ok {
			problems = append(problems, fmt.Sprintf("%s:%d: the string literal %s holds %q, which names every environment variable of this process, credentials included; a composition root, and the package it hands the work to, read the named variable they need and nothing else", rel, fset.Position(lit.Pos()).Line, lit.Value, path))
		}
		return true
	})
	return problems, nil
}

// TestRootsAndCLINameNoEnvironmentPath checks every Go file of the
// composition root and of the packages it hands the work to, internal/cli and
// internal/adapter/source/inventoryfile, _test.go files included, for a
// literal that names the environment of a process (environmentPathProblems).
// The tree holds none today, and the test pins that:
// os.ReadFile("/proc/self/environ") in a composition root, or in a package it
// hands the work to, would hand out every variable of the process,
// credentials included, and no rule of the forbidigo groups catches a read of
// a path.
func TestRootsAndCLINameNoEnvironmentPath(t *testing.T) {
	t.Parallel()

	root := moduleRoot(t)
	files := 0
	refused, err := walkGoFiles(root, skipDir, func(rel string, src []byte) error {
		if !slices.ContainsFunc(environmentPathDirs, func(dir string) bool {
			return strings.HasPrefix(rel, dir+"/")
		}) {
			return nil
		}
		files++
		problems, err := environmentPathProblems(rel, src)
		if err != nil {
			return err
		}
		for _, p := range problems {
			t.Error(p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	for _, p := range refused {
		t.Error(p)
	}
	if files == 0 {
		t.Fatalf("no Go file under %v", environmentPathDirs)
	}
}

// TestEnvironmentPathProblems pins environmentPathProblems, the seam of
// TestRootsAndCLINameNoEnvironmentPath, on fixtures: the tree holds no such
// literal, so a fixture is what proves the check would refuse one. A case's rel
// is the file the literal is read in: a root's own Go file, or its _test.go,
// which the check holds alike.
func TestEnvironmentPathProblems(t *testing.T) {
	t.Parallel()

	const rel = "cmd/shift2eu/read.go"
	tests := []struct {
		name string
		rel  string
		src  string
		want []string
	}{
		{
			name: "a path of the root's own",
			src:  "package main\n\nvar config = \"/etc/shift2eu/config.json\"\n",
		},
		{
			name: "the environment of this process",
			src:  "package main\n\nimport \"os\"\n\nfunc read() { _, _ = os.ReadFile(\"/proc/self/environ\") }\n",
			want: []string{
				rel + ":5: the string literal \"/proc/self/environ\" holds \"proc\", which names every environment variable of this process, credentials included; a composition root, and the package it hands the work to, read the named variable they need and nothing else",
			},
		},
		{
			name: "the environment of this process, as a raw string",
			src:  "package main\n\nimport \"os\"\n\nfunc read() { _, _ = os.ReadFile(`/proc/self/environ`) }\n",
			want: []string{
				rel + ":5: the string literal `/proc/self/environ` holds \"proc\", which names every environment variable of this process, credentials included; a composition root, and the package it hands the work to, read the named variable they need and nothing else",
			},
		},
		{
			name: "a descriptor of this process",
			src:  "package main\n\nimport \"os\"\n\nfunc read() { _, _ = os.ReadFile(\"/dev/fd/3\") }\n",
			want: []string{
				rel + ":5: the string literal \"/dev/fd/3\" holds \"dev/fd\", which names every environment variable of this process, credentials included; a composition root, and the package it hands the work to, read the named variable they need and nothing else",
			},
		},
		{
			// The round-2 review's finding L-C: os.DirFS("/") with a path
			// relative to the root names the same file without the leading
			// slash, and a literal check that only looks for "/proc/" misses
			// it.
			name: "the environment of this process, without the leading slash",
			src:  "package main\n\nimport \"os\"\n\nfunc read() { _, _ = os.ReadFile(\"proc/self/environ\") }\n",
			want: []string{
				rel + ":5: the string literal \"proc/self/environ\" holds \"proc\", which names every environment variable of this process, credentials included; a composition root, and the package it hands the work to, read the named variable they need and nothing else",
			},
		},
		{
			name: "a descriptor of this process, without the leading slash",
			src:  "package main\n\nimport \"os\"\n\nfunc read() { _, _ = os.ReadFile(\"dev/fd/3\") }\n",
			want: []string{
				rel + ":5: the string literal \"dev/fd/3\" holds \"dev/fd\", which names every environment variable of this process, credentials included; a composition root, and the package it hands the work to, read the named variable they need and nothing else",
			},
		},
		{
			// The directory itself, with no trailing slash: os.OpenRoot("/proc"),
			// io.ReadAll over os.DirFS("/proc").Open("self/environ"), or
			// os.ReadDir("/dev/fd"), each of which names an environment the
			// path check with a trailing slash missed (the auditor's finding L-1).
			name: "the environment of this process, named without a trailing slash",
			src:  "package main\n\nimport \"os\"\n\nfunc read() { _, _ = os.ReadDir(\"/proc\") }\n",
			want: []string{
				rel + ":5: the string literal \"/proc\" holds \"proc\", which names every environment variable of this process, credentials included; a composition root, and the package it hands the work to, read the named variable they need and nothing else",
			},
		},
		{
			name: "the environment of this process, without the leading slash or the trailing one",
			src:  "package main\n\nimport \"os\"\n\nfunc open() { _, _ = os.OpenRoot(\"proc\") }\n",
			want: []string{
				rel + ":5: the string literal \"proc\" holds \"proc\", which names every environment variable of this process, credentials included; a composition root, and the package it hands the work to, read the named variable they need and nothing else",
			},
		},
		{
			name: "a descriptor directory, named without a trailing slash",
			src:  "package main\n\nimport \"os\"\n\nfunc read() { _, _ = os.Open(\"/dev/fd\") }\n",
			want: []string{
				rel + ":5: the string literal \"/dev/fd\" holds \"dev/fd\", which names every environment variable of this process, credentials included; a composition root, and the package it hands the work to, read the named variable they need and nothing else",
			},
		},
		{
			name: "the environment of this process, with a trailing slash only",
			src:  "package main\n\nimport \"os\"\n\nfunc read() { _, _ = os.ReadDir(\"/proc/\") }\n",
			want: []string{
				rel + ":5: the string literal \"/proc/\" holds \"proc\", which names every environment variable of this process, credentials included; a composition root, and the package it hands the work to, read the named variable they need and nothing else",
			},
		},
		{
			// An entry matches as a whole path segment, so a name that only
			// ends in one is no such path, and neither is a directory whose name
			// merely starts with one.
			name: "a name that only ends in one is no path",
			src:  "package main\n\nimport \"os\"\n\nfunc read() { _, _ = os.ReadFile(\"myproc/self/environ\") }\n",
		},
		{
			name: "a directory that only starts with one is no path",
			src:  "package main\n\nimport \"os\"\n\nfunc read() { _, _ = os.ReadFile(\"/procfs/self/environ\") }\n",
		},
		{
			name: "a test file is held to it too",
			rel:  "cmd/shift2eu/read_test.go",
			src:  "package main\n\nimport \"os\"\n\nvar _ = os.ReadFile(\"/proc/1/environ\")\n",
			want: []string{
				"cmd/shift2eu/read_test.go:5: the string literal \"/proc/1/environ\" holds \"proc\", which names every environment variable of this process, credentials included; a composition root, and the package it hands the work to, read the named variable they need and nothing else",
			},
		},
		{
			name: "a comment is no read",
			src:  "package main\n\n// os.ReadFile(\"/proc/self/environ\") would hand out every variable.\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			at := tt.rel
			if at == "" {
				at = rel
			}

			got, err := environmentPathProblems(at, []byte(tt.src))
			if err != nil {
				t.Fatalf("environmentPathProblems(): %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("environmentPathProblems() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
