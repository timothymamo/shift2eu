package markdown_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/timothymamo/shift2eu/internal/adapter/report/markdown"
	"github.com/timothymamo/shift2eu/internal/domain/estimate"
)

// domainEstimateDir is the estimate domain package's source, relative to this
// package's own directory: go test runs a package's tests in its source
// directory.
const domainEstimateDir = "../../../domain/estimate"

// phraseRegion is the region the reasons that name one are rendered with.
const phraseRegion = "fr-par"

// reasonPhrases is the phrase a report gives each reason of the estimate domain,
// as the Monthly estimate cell and a total show it, with the estimate whose
// region the phrase names. It is the report's own wording, so it stays in this
// adapter (CLAUDE.md, ubiquitous language: the domain gives reasons, not
// sentences); a reason the domain declares without an entry here is named by
// TestEveryReasonHasAPhraseAndAPlaceInTheOrder.
var reasonPhrases = map[estimate.Reason]struct {
	estimate estimate.Estimate
	want     string
}{
	estimate.NoFit:             {want: "no fit"},
	estimate.UsagePriced:       {want: "usage-priced"},
	estimate.NoSize:            {want: "size unknown"},
	estimate.NoPlan:            {want: "no plan"},
	estimate.NoPlanLargeEnough: {want: "no plan large enough"},
	estimate.NotOffered:        {estimate: estimate.Estimate{Region: phraseRegion}, want: "not offered in `" + phraseRegion + "`"},
	estimate.NoPrice:           {estimate: estimate.Estimate{Region: phraseRegion}, want: "no price in `" + phraseRegion + "`"},
	estimate.NoStoragePrice:    {estimate: estimate.Estimate{Region: phraseRegion}, want: "no storage price in `" + phraseRegion + "`"},
}

// declaredReasons returns every estimate.Reason constant the domain declares,
// in declaration order, read from the domain's source. The domain is the one
// place that names a reason (CLAUDE.md, ubiquitous language), and it declares
// them as typed constants and nothing else, so parsing it is how a test sees a
// reason it was not written for: a reason added there without a phrase here must
// fail this test, not wait for someone to remember this file. It reads files
// only, in a deterministic order, and needs no network.
func declaredReasons(t *testing.T) []estimate.Reason {
	t.Helper()

	entries, err := os.ReadDir(domainEstimateDir)
	if err != nil {
		t.Fatalf("reading %s returned unexpected error: %v", domainEstimateDir, err)
	}

	fset := token.NewFileSet()

	var reasons []estimate.Reason

	// ReadDir returns the entries sorted by name, so the reasons are visited in a
	// defined order.
	for _, entry := range entries {
		name := entry.Name()

		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, filepath.Join(domainEstimateDir, name), nil, 0)
		if err != nil {
			t.Fatalf("parsing %s returned unexpected error: %v", name, err)
		}

		for _, decl := range file.Decls {
			reasons = append(reasons, constantReasons(t, fset, decl)...)
		}
	}

	return reasons
}

// constantReasons returns the values of the estimate.Reason constants decl
// declares, in source order, and nothing for a declaration that declares none.
func constantReasons(t *testing.T, fset *token.FileSet, decl ast.Decl) []estimate.Reason {
	t.Helper()

	gen, ok := decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.CONST {
		return nil
	}

	var reasons []estimate.Reason

	// typed is set by a spec that spells the type out and inherited by the specs
	// after it, which repeat it by giving no type of their own.
	typed := false

	for _, spec := range gen.Specs {
		value, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}

		if ident, ok := value.Type.(*ast.Ident); ok {
			typed = ident.Name == "Reason"
		}

		if !typed {
			continue
		}

		for _, expr := range value.Values {
			lit, ok := expr.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Fatalf("%s: estimate.Reason constant %s is not a string literal, want one this test can read",
					fset.Position(lit.Pos()), value.Names[0].Name)
			}

			reason, err := strconv.Unquote(lit.Value)
			if err != nil {
				t.Fatalf("%s: unquoting estimate.Reason %s returned unexpected error: %v",
					fset.Position(lit.Pos()), value.Names[0].Name, err)
			}

			reasons = append(reasons, estimate.Reason(reason))
		}
	}

	return reasons
}

// TestEveryReasonHasAPhraseAndAPlaceInTheOrder pins that the report says
// something about every reason the estimate domain can give a resource, and
// names it in reasonOrder: a reason added to the domain without a phrase here, or
// without a place in the order, would be silently dropped from "none of N
// resources could be estimated (<reasons>)" (issue #308). The phrasing stays in
// this adapter; the domain gives reasons, not sentences.
func TestEveryReasonHasAPhraseAndAPlaceInTheOrder(t *testing.T) {
	t.Parallel()

	reasons := declaredReasons(t)

	if len(reasons) == 0 {
		t.Fatalf("no estimate.Reason constant found in %s, want the domain's reasons", domainEstimateDir)
	}

	for _, reason := range reasons {
		t.Run(string(reason), func(t *testing.T) {
			t.Parallel()

			if !slices.Contains(markdown.ReasonOrder, reason) {
				t.Errorf("reasonOrder has no place for estimate.Reason %q, want it in %v so a total names it",
					string(reason), markdown.ReasonOrder)
			}

			phrase, ok := reasonPhrases[reason]
			if !ok {
				t.Errorf("estimate.Reason %q has no Markdown phrase, want one in this test's reasonPhrases "+
					"and a case in the report's renderNotEstimated", string(reason))

				return
			}

			e := phrase.estimate
			e.Reason = reason

			if got := markdown.RenderNotEstimated(e, false); got != phrase.want {
				t.Errorf("renderNotEstimated(%q) = %q, want %q", string(reason), got, phrase.want)
			}
		})
	}
}

// TestReasonOrderHoldsOnlyDomainReasons pins the other direction: reasonOrder
// names no reason the domain no longer declares, so the report's order cannot
// quietly hold a stale entry.
func TestReasonOrderHoldsOnlyDomainReasons(t *testing.T) {
	t.Parallel()

	reasons := declaredReasons(t)

	for _, reason := range markdown.ReasonOrder {
		if !slices.Contains(reasons, reason) {
			t.Errorf("reasonOrder lists estimate.Reason %q, which the domain does not declare; want one of %v",
				string(reason), reasons)
		}
	}
}
