package pdfreport_test

// The reproducibility rules: the same input written twice gives the same bytes,
// and no production file of the package holds floating-point arithmetic, which
// could round differently on arm64 than on amd64 or js/wasm (ADR 0049).

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/app/analysis"
	"github.com/timothymamo/shift2eu/internal/domain/assessment"
	"github.com/timothymamo/shift2eu/internal/testutil"
)

// TestReportIsDeterministic checks that the same assessment and metadata written
// twice give the same bytes, so no clock, random source or map order reaches the
// file.
func TestReportIsDeterministic(t *testing.T) {
	t.Parallel()

	meta := analysis.ReportMeta{InventoryName: inventoryFileName}

	tests := []struct {
		name   string
		render func(tb testing.TB) []byte
	}{
		{
			name: "single-target report with estimates",
			render: func(tb testing.TB) []byte {
				tb.Helper()

				return renderReport(tb, estimateAssessment(tb, "", true), meta)
			},
		},
		{
			name: "single-target report without estimates",
			render: func(tb testing.TB) []byte {
				tb.Helper()

				return renderReport(tb, estimateAssessment(tb, "", false), meta)
			},
		},
		{
			name: "comparison of three targets",
			render: func(tb testing.TB) []byte {
				tb.Helper()

				c := assessment.Compare(assessTargets(tb, newInventory(tb, sourceName, comparisonResources()), threeTargetKeys()))

				return renderComparison(tb, c, meta)
			},
		},
		{
			name: "hostile report",
			render: func(tb testing.TB) []byte {
				tb.Helper()

				a := assessment.Assess(newInventory(tb, hostileSource, hostileResources()), hostileCatalog(tb, hostileTarget))

				return renderReport(tb, a, analysis.ReportMeta{InventoryName: hostileInventoryName})
			},
		},
		{
			name: "empty inventory",
			render: func(tb testing.TB) []byte {
				tb.Helper()

				a := assessment.AssessWith(newInventory(tb, sourceName, nil), testutil.ReportCatalog(tb), assessment.Options{Estimate: true})

				return renderReport(tb, a, meta)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			first := test.render(t)
			second := test.render(t)

			if !bytes.Equal(first, second) {
				t.Errorf("the same input written twice gives different bytes: %d and %d bytes; the first difference is at %d",
					len(first), len(second), firstDifference(first, second))
			}
		})
	}
}

// firstDifference returns the offset of the first byte two renderings differ at,
// or the shorter length when one is a prefix of the other.
func firstDifference(first, second []byte) int {
	for i := range min(len(first), len(second)) {
		if first[i] != second[i] {
			return i
		}
	}

	return min(len(first), len(second))
}

// TestNoFloatingPoint fails when a non-test file of pdfreport imports math,
// declares a floating-point type, holds a floating-point literal, calls
// strconv.FormatFloat or strconv.ParseFloat, or uses a floating-point format
// verb (e, E, f, F, g, G, x or X, with any argument index, flags, width or
// precision): integer layout arithmetic keeps the bytes identical on every
// release target (ADR 0049).
func TestNoFloatingPoint(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the package directory: %v", err)
	}

	checked := 0

	for _, entry := range entries {
		name := entry.Name()

		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		checked++

		// The path is the package's own file, a name the directory gave, never
		// user input.
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}

		files := token.NewFileSet()

		file, err := parser.ParseFile(files, name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		checkNoFloatingPoint(t, files, file)
	}

	if checked == 0 {
		t.Fatal("pdfreport holds no production file, so there is nothing to check")
	}
}

// checkNoFloatingPoint checks one parsed file for floating-point arithmetic and
// for the format verbs that would print a float.
func checkNoFloatingPoint(t *testing.T, files *token.FileSet, file *ast.File) {
	t.Helper()

	where := func(pos token.Pos) string {
		return files.Position(pos).String()
	}

	for _, spec := range file.Imports {
		path := strings.Trim(spec.Path.Value, `"`)
		if path == "math" || strings.HasPrefix(path, "math/") {
			t.Errorf("%s imports %q, which the integer layout arithmetic may not use", where(spec.Pos()), path)
		}
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.BasicLit:
			if n.Kind == token.FLOAT {
				t.Errorf("%s holds the floating-point literal %s", where(n.Pos()), n.Value)
			}

			if n.Kind == token.STRING && holdsFloatVerb(n.Value) {
				t.Errorf("%s holds the string %s, which uses a floating-point format verb", where(n.Pos()), n.Value)
			}
		case *ast.Ident:
			if n.Name == "float32" || n.Name == "float64" {
				t.Errorf("%s uses the floating-point type %s", where(n.Pos()), n.Name)
			}
		case *ast.CallExpr:
			selector, ok := n.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}

			receiver, ok := selector.X.(*ast.Ident)
			if !ok || receiver.Name != "strconv" {
				return true
			}

			if selector.Sel.Name == "FormatFloat" || selector.Sel.Name == "ParseFloat" {
				t.Errorf("%s calls strconv.%s", where(n.Pos()), selector.Sel.Name)
			}
		}

		return true
	})
}

// holdsFloatVerb reports whether a quoted Go string literal holds a format
// directive that prints a floating-point number: the verbs e, E, f, F, g, G, x
// and X, whatever explicit argument indexes, flags, width and precision are
// written before them. A %% is a literal percent sign, never a verb, so a
// literal holding "%%EOF" holds no directive at all.
//
// It reads the literal's source text and nothing else: it cannot see the type
// of the operand a %v (or %+v, %#v) directive prints, so it cannot tell a %v of
// a float from a %v of an error. That blind spot is covered by the rest of
// checkNoFloatingPoint: a package that names no float type, holds no
// floating-point literal, calls neither strconv.FormatFloat nor
// strconv.ParseFloat and imports no math package has no float to show.
func holdsFloatVerb(literal string) bool {
	for i := 0; i < len(literal); i++ {
		if literal[i] != '%' {
			continue
		}

		if i+1 < len(literal) && literal[i+1] == '%' {
			// A literal percent sign: what follows it is text, never a verb.
			i++

			continue
		}

		if verb, ok := formatVerb(literal[i+1:]); ok && isFloatVerb(verb) {
			return true
		}
	}

	return false
}

// isFloatVerb reports whether a format verb prints a floating-point number: e,
// E, f, F, g and G print it as one, and x and X print its bits.
func isFloatVerb(verb byte) bool {
	switch verb {
	case 'e', 'E', 'f', 'F', 'g', 'G', 'x', 'X':
		return true
	default:
		return false
	}
}

// formatVerb returns the verb of the format directive that starts rest, the
// text after a '%', and whether rest holds one at all. It skips what Go's fmt
// package allows between the percent sign and the verb: an explicit argument
// index (%[1]f), the flags +, -, #, space and 0, then the width and the
// precision, either of which may be a * (with an argument index of its own,
// %[2]*.[1]f) instead of digits.
func formatVerb(rest string) (byte, bool) {
	i := skipArgIndex(rest, 0)

	for i < len(rest) && strings.IndexByte("+-# 0", rest[i]) >= 0 {
		i++
	}

	i = skipWidth(rest, i)

	if i < len(rest) && rest[i] == '.' {
		i = skipWidth(rest, i+1)
	}

	if i >= len(rest) {
		return 0, false
	}

	return rest[i], true
}

// skipWidth skips the width or the precision written at i: digits, or a * that
// takes its value from an operand, each with an argument index of its own after
// it.
func skipWidth(rest string, i int) int {
	for i < len(rest) {
		switch {
		case rest[i] >= '0' && rest[i] <= '9':
			i++
		case rest[i] == '*':
			i = skipArgIndex(rest, i+1)
		case rest[i] == '[':
			i = skipArgIndex(rest, i)
		default:
			return i
		}
	}

	return i
}

// skipArgIndex skips the explicit argument index [n] written at i, and a
// truncated index to the end of rest.
func skipArgIndex(rest string, i int) int {
	if i >= len(rest) || rest[i] != '[' {
		return i
	}

	end := strings.IndexByte(rest[i:], ']')
	if end < 0 {
		return len(rest)
	}

	return i + end + 1
}

// TestHoldsFloatVerb pins the guard's reading of a format directive: every verb
// that prints a float, however its argument indexes, flags, width and precision
// are written, and no directive that prints something else.
func TestHoldsFloatVerb(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		format string
		want   bool
	}{
		{name: "a literal percent sign", format: "%%"},
		{name: "a literal percent sign before an E", format: "%%E"},
		{name: "a literal percent sign before an f", format: "%%f"},
		{name: "the end-of-file marker with no directive", format: "%%EOF\n"},
		{name: "a string without a percent sign", format: "EOF"},
		{name: "a percent sign in the middle of a sentence", format: "100% of the report"},
		{name: "the f verb", format: "%f", want: true},
		{name: "the E verb", format: "%E", want: true},
		{name: "the F verb", format: "%F", want: true},
		{name: "the G verb", format: "%G", want: true},
		{name: "the x verb", format: "%x", want: true},
		{name: "the X verb", format: "%X", want: true},
		{name: "a precision", format: "%.2f", want: true},
		{name: "a width and a precision", format: "%8.3f", want: true},
		{name: "the plus flag", format: "%+e", want: true},
		{name: "a left-justified width and a precision", format: "%-10.4g", want: true},
		{name: "an argument index", format: "%[1]f", want: true},
		{name: "an indexed width and an indexed precision", format: "%[2]*.[1]f", want: true},
		{name: "the d verb", format: "%d"},
		{name: "the s verb", format: "%s"},
		{name: "the q verb", format: "%q"},
		{name: "the v verb, whose operand the guard cannot see", format: "%v"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			literal := strconv.Quote(test.format)
			if got := holdsFloatVerb(literal); got != test.want {
				t.Errorf("holdsFloatVerb(%s) = %t, want %t", literal, got, test.want)
			}
		})
	}
}
