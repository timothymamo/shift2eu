// Guards against a test of this package running make other than through makeProgram (issue #314). It catches a
// literal "make" or "gmake" handed to os/exec, not a program name built any other way: a literal exec.Command("make", ...) or
// exec.CommandContext(ctx, "make", ...) runs the make first on PATH, never the one SHIFT2EU_TEST_MAKE names, so
// that test would pass in CI's make 3.81 job under make 4.x and say nothing about the Mac's /usr/bin/make.
package devenv_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// literalMakeCalls returns the position of every call in file that hands os/exec's Command or CommandContext the
// program as a string literal make (or gmake): the first argument of Command, the second of CommandContext. It
// reads the package name exec as os/exec, the name every file of this package imports it under.
func literalMakeCalls(fset *token.FileSet, file *ast.File) []token.Position {
	var found []token.Position

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "exec" {
			return true
		}

		at := -1

		switch sel.Sel.Name {
		case "Command":
			at = 0
		case "CommandContext":
			at = 1
		}

		if at < 0 || len(call.Args) <= at {
			return true
		}

		lit, ok := call.Args[at].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}

		if program, err := strconv.Unquote(lit.Value); err == nil && (program == "make" || program == "gmake") {
			found = append(found, fset.Position(call.Pos()))
		}

		return true
	})

	return found
}

// TestTestsRunMakeThroughMakeProgram scans every test file of this package for a literal "make" or "gmake"
// handed to exec.Command or exec.CommandContext, a guard against a test that runs make by accident: the makes
// the tests run should come from makeProgram, so CI's make 3.81 job runs them under /usr/bin/make. It does not
// catch a program name built any other way (issue #314).
func TestTestsRunMakeThroughMakeProgram(t *testing.T) {
	t.Parallel()

	files, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("list the package's test files: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("found no test file of the package to scan, so the check would pass on nothing")
	}

	fset := token.NewFileSet()

	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		file, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		for _, pos := range literalMakeCalls(fset, file) {
			t.Errorf("%s runs a literal make: run makeProgram(t) instead, so CI's make 3.81 job runs this make under the Mac's /usr/bin/make and not the make first on PATH (issue #314)", pos)
		}
	}
}

// TestLiteralMakeCallsFindsEachForm pins the scan on a source of its own: it must find a literal make in both
// calls, in gmake's spelling too, and leave makeProgram's call and every other program alone, so the scan above
// cannot pass because it finds nothing at all.
func TestLiteralMakeCallsFindsEachForm(t *testing.T) {
	t.Parallel()

	const src = `package probe

import (
	"context"
	"os/exec"
	"testing"
)

func probe(t *testing.T, ctx context.Context) {
	_ = exec.Command("make", "-C", "x")
	_ = exec.CommandContext(ctx, "make", "vm-up")
	_ = exec.Command("gmake")
	_ = exec.Command(makeProgram(t), "-C", "x")
	_ = exec.CommandContext(ctx, makeProgram(t))
	_ = exec.Command("git", "make")
	_ = exec.CommandContext(ctx, "sh", "-c", "make")
}
`

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "probe.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse the probe: %v", err)
	}

	var lines []string
	for _, pos := range literalMakeCalls(fset, file) {
		lines = append(lines, strconv.Itoa(pos.Line))
	}

	if got, want := strings.Join(lines, ","), "10,11,12"; got != want {
		t.Errorf("the scan finds a literal make on the probe's lines %q, want %q: the two calls with make and the one with gmake, and no other", got, want)
	}
}
