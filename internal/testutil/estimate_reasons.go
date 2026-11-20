package testutil

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/domain/estimate"
)

// This file holds the reader the report guards share. A report names a reason
// the estimate domain gives, in its own words and in its own order, and the
// domain is the one place that declares a reason (CLAUDE.md, ubiquitous
// language). So a guard reads the reasons from the domain's source: a reason
// added there without a phrase and a place in a report's order must fail that
// report's test, not wait for someone to remember to extend it. Reading files
// only, in a fixed order, it needs no network and no credentials.

// EstimateReasons returns every estimate.Reason constant the estimate domain
// declares, in declaration order, read from the package's source. It fails tb
// when it cannot read that source, when a constant typed Reason has a value it
// cannot read as a string literal (estimateReasonsInSource says which and why),
// or when the domain declares no reason at all, so a reader that looked in the
// wrong directory can never pass a guard vacuously.
//
// The directory is found from this file's own path: go test compiles the test
// binary from the source checkout, so runtime.Caller names the checkout's
// internal/testutil, and the domain is its sibling package.
func EstimateReasons(tb testing.TB) []estimate.Reason {
	tb.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("cannot name this source file, so cannot find the estimate domain")
	}

	dir := filepath.Join(filepath.Dir(file), "..", "domain", "estimate")

	reasons := estimateReasonsInDir(tb, dir)
	if len(reasons) == 0 {
		tb.Fatalf("no estimate.Reason constant found in %s, want the domain's reasons", dir)
	}

	return reasons
}

// estimateReasonsInDir returns the reasons the estimate domain's Go files in
// dir declare, in the order os.ReadDir gives the files (sorted by name) and in
// declaration order within a file.
func estimateReasonsInDir(tb testing.TB, dir string) []estimate.Reason {
	tb.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		tb.Fatalf("reading %s returned unexpected error: %v", dir, err)
	}

	var reasons []estimate.Reason

	for _, entry := range entries {
		name := entry.Name()

		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		path := filepath.Join(dir, name)

		src, err := os.ReadFile(path) //nolint:gosec // the estimate domain's own source, not user input
		if err != nil {
			tb.Fatalf("reading %s returned unexpected error: %v", path, err)
		}

		found, err := estimateReasonsInSource(name, src)
		if err != nil {
			tb.Fatalf("%s: %v", path, err)
		}

		reasons = append(reasons, found...)
	}

	return reasons
}

// estimateReasonsInSource returns the reasons the estimate.Reason constants in
// src declare, in declaration order. name is the file's name, for the positions
// a failure reports.
//
// A constant is an estimate.Reason when its spec spells the type out, or when
// its value is a conversion to Reason, or when it repeats the spec above it
// (see estimateReasons). A spec with values and no type is untyped, even below
// a typed spec: Go gives each of its expressions its default type, so a string
// literal there is a string and not a reason. A spec the test reads as a reason
// whose value is not a string literal is an error naming the constant: skipping
// it would leave that reason unguarded.
func estimateReasonsInSource(name string, src []byte) ([]estimate.Reason, error) {
	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, name, src, 0)
	if err != nil {
		return nil, fmt.Errorf("parsing the source: %w", err)
	}

	var reasons []estimate.Reason

	for _, decl := range file.Decls {
		found, err := constantReasons(fset, decl)
		if err != nil {
			return nil, err
		}

		reasons = append(reasons, found...)
	}

	return reasons, nil
}

// constantReasons returns the reasons the constants decl declares, in source
// order, and nothing for a declaration that declares none or that is not a
// const declaration.
//
// A spec's type is inherited only by the specs after it that give neither a
// type nor values, which Go reads as a textual repetition of the spec above:
// their type and their values are the ones above, so a typed spec followed by
// such a spec leaves a Reason whose value the test cannot read. A spec with
// values of its own takes no type from above, so only a value that is itself a
// conversion to Reason (Reason("...")) makes it a reason.
func constantReasons(fset *token.FileSet, decl ast.Decl) ([]estimate.Reason, error) {
	gen, ok := decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.CONST {
		return nil, nil
	}

	var reasons []estimate.Reason

	// reasonAbove reports whether the spec above this one declares a reason, so
	// this one, when it repeats it, is a reason too.
	reasonAbove := false

	for _, spec := range gen.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}

		switch {
		case vs.Type != nil:
			// A spec that spells its type out takes nothing from above, whatever
			// it declares: the type decides.
			ident, isIdent := vs.Type.(*ast.Ident)
			reasonAbove = isIdent && ident.Name == "Reason"

			if !reasonAbove {
				continue
			}

			for i, expr := range vs.Values {
				reason, err := reasonLiteral(fset, expr, valueName(vs, i))
				if err != nil {
					return nil, err
				}

				reasons = append(reasons, reason)
			}
		case len(vs.Values) == 0:
			// No type and no values: Go repeats the spec above, type and values
			// both. That is a reason when the spec above was one, and its value
			// is one the test does not see written here, so reading it fails.
			if reasonAbove {
				return nil, fmt.Errorf("%s: estimate.Reason constant %s repeats the spec above it, so its value is "+
					"not written here; want each reason written as a string literal",
					fset.Position(vs.Pos()), vs.Names[0].Name)
			}
		default:
			// A spec with values and no type declares untyped constants, but a
			// value that is a conversion to Reason names a reason itself.
			found := false

			for i, expr := range vs.Values {
				if !isReasonConversion(expr) {
					continue
				}

				reason, err := reasonLiteral(fset, expr, valueName(vs, i))
				if err != nil {
					return nil, err
				}

				reasons = append(reasons, reason)

				found = true
			}

			reasonAbove = found
		}
	}

	return reasons, nil
}

// valueName returns the name of the spec's ith constant, for a failure message.
// Go pairs a spec's names with its values in order, so the ith value is the ith
// name's.
func valueName(vs *ast.ValueSpec, i int) string {
	if i < len(vs.Names) {
		return vs.Names[i].Name
	}

	return vs.Names[len(vs.Names)-1].Name
}

// isReasonConversion reports whether expr is a conversion to Reason with one
// argument: Reason("...").
func isReasonConversion(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return false
	}

	ident, ok := call.Fun.(*ast.Ident)

	return ok && ident.Name == "Reason"
}

// reasonLiteral returns the reason expr declares, named name in a failure
// message: the string literal expr is, or the string literal a conversion to
// Reason around it wraps. Any other value, such as iota, a concatenation or
// another constant, fails: a reason the test cannot read must not be skipped.
func reasonLiteral(fset *token.FileSet, expr ast.Expr, name string) (estimate.Reason, error) {
	value := expr
	if call, ok := expr.(*ast.CallExpr); ok && isReasonConversion(call) {
		value = call.Args[0]
	}

	literal, ok := value.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", fmt.Errorf("%s: estimate.Reason constant %s is not a string literal, or a conversion "+
			"Reason(\"...\") of one, which is the only value a report guard can read; write it as one",
			fset.Position(expr.Pos()), name)
	}

	reason, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", fmt.Errorf("%s: unquoting estimate.Reason %s returned unexpected error: %w",
			fset.Position(literal.Pos()), name, err)
	}

	return estimate.Reason(reason), nil
}
