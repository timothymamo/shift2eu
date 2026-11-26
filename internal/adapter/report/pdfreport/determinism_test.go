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
// strconv.FormatFloat or strconv.ParseFloat, or uses a %f, %F, %g, %G, %e or %E
// verb: integer layout arithmetic keeps the bytes identical on every release
// target (ADR 0049).
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

// holdsFloatVerb reports whether a quoted Go string literal holds a %f, %F, %g,
// %G, %e or %E verb.
func holdsFloatVerb(literal string) bool {
	for i := 0; i+1 < len(literal); i++ {
		if literal[i] != '%' {
			continue
		}

		switch literal[i+1] {
		case 'f', 'F', 'g', 'G', 'e', 'E':
			return true
		}
	}

	return false
}
